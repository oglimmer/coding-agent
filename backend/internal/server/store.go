package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ErrNotFound is returned by Store lookups when no row matches.
var ErrNotFound = errors.New("not found")

// Role is a user's privilege level. Ordered viewer < user < admin: a viewer can
// only read, a user can additionally create jobs, an admin can manage repos and
// other users. New accounts start as viewers and must be promoted by an admin.
type Role string

const (
	RoleViewer Role = "viewer"
	RoleUser   Role = "user"
	RoleAdmin  Role = "admin"
)

// Valid reports whether r is one of the known roles.
func (r Role) Valid() bool {
	return r == RoleViewer || r == RoleUser || r == RoleAdmin
}

// CanWrite reports whether the role may create or mutate its own resources
// (i.e. submit feature requests). Viewers are read-only.
func (r Role) CanWrite() bool {
	return r == RoleUser || r == RoleAdmin
}

// User is a platform account.
type User struct {
	ID           string    `json:"id"`
	OIDCSub      string    `json:"-"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Role         Role      `json:"role"`
	TokenVersion int       `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

// IsAdmin reports whether the user holds the admin role.
func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

// CanWrite reports whether the user may create or mutate their own resources.
func (u User) CanWrite() bool { return u.Role.CanWrite() }

// Repo is a GitHub repository users can target.
type Repo struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	Name       string `json:"name"`
	BaseBranch string `json:"baseBranch"`
	// VerifyCommand is the repo's authoritative build/lint/test command; the worker
	// runs it as a hard gate before opening a PR. Empty = the worker detects one.
	VerifyCommand string `json:"verifyCommand"`
	// TestCommand is the fast inner-loop command the worker feeds aider --auto-test
	// after every edit. Empty = the worker detects it from the repo's manifests.
	TestCommand string    `json:"testCommand"`
	AddedBy     *string   `json:"addedBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// FullName returns "owner/name".
func (r Repo) FullName() string { return r.Owner + "/" + r.Name }

// Job is one feature request and its lifecycle.
type Job struct {
	ID       string `json:"id"`
	RepoID   string `json:"repoId"`
	RepoName string `json:"repoName"`
	UserID   string `json:"userId"`
	UserName string `json:"userName"`
	Feature  string `json:"feature"`
	Status   string `json:"status"`
	Engine   string `json:"engine"` // "aider" | "claude-code"
	// Model is the coding model the job ran on (aider architect / claude-code
	// primary); EditorModel is aider's editor model (empty for claude-code).
	// Empty means the engine's deployment default was used.
	Model       string `json:"model,omitempty"`
	EditorModel string `json:"editorModel,omitempty"`
	K8sJobName  string `json:"-"`
	Branch      string `json:"branch,omitempty"`
	PRURL       string `json:"prUrl,omitempty"`
	Reason      string `json:"reason,omitempty"`
	// Metadata is the config snapshot captured when the job was created (platform
	// commit, models, review rounds, verify command, …). Small; kept in list rows.
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// Store is the data-access layer. Read queries live here; transactional writes
// may use a.DB directly.
type Store struct {
	DB *sql.DB
}

// --- users -------------------------------------------------------------------

// UpsertOIDCUser inserts or updates a user identified by their OIDC subject.
// The first user ever created becomes an admin. Returns the resolved user.
func (s *Store) UpsertOIDCUser(ctx context.Context, sub, email, name string) (User, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return User{}, err
	}
	// The very first account bootstraps as admin; everyone else starts as a
	// read-only viewer and must be promoted.
	role := RoleViewer
	if count == 0 {
		role = RoleAdmin
	}

	u, err := scanUser(tx.QueryRowContext(ctx, `
		INSERT INTO users (oidc_sub, email, name, role)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (oidc_sub) DO UPDATE
		  SET email = EXCLUDED.email, name = EXCLUDED.name
		RETURNING `+userCols,
		sub, email, name, string(role),
	))
	if err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return u, nil
}

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var sub sql.NullString
	var role string
	err := row.Scan(&u.ID, &sub, &u.Email, &u.Name, &role, &u.TokenVersion, &u.CreatedAt)
	u.OIDCSub = sub.String
	u.Role = Role(role)
	return u, err
}

const userCols = `id, oidc_sub, email, name, role, token_version, created_at`

// UserByID resolves a user by primary key.
func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	u, err := scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// ListUsers returns all users, newest first.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountAdmins returns how many admins exist.
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role = 'admin'`).Scan(&n)
	return n, err
}

// SetRole changes a user's role and bumps token_version so existing sessions
// re-resolve their privileges on the next request. Returns the updated user.
func (s *Store) SetRole(ctx context.Context, id string, role Role) (User, error) {
	u, err := scanUser(s.DB.QueryRowContext(ctx, `
		UPDATE users SET role = $2, token_version = token_version + 1
		WHERE id = $1
		RETURNING `+userCols, id, string(role)))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// UpsertPasswordUser is the dev-only login path: it resolves (or creates) a
// user keyed by a synthetic subject. First user becomes admin.
func (s *Store) UpsertPasswordUser(ctx context.Context, username string) (User, error) {
	return s.UpsertOIDCUser(ctx, "dev:"+username, username+"@dev.local", username)
}

// --- repos -------------------------------------------------------------------

const repoCols = `id, owner, name, base_branch, verify_command, test_command, added_by, created_at`

func scanRepo(row interface{ Scan(...any) error }) (Repo, error) {
	var r Repo
	var addedBy sql.NullString
	err := row.Scan(&r.ID, &r.Owner, &r.Name, &r.BaseBranch, &r.VerifyCommand, &r.TestCommand, &addedBy, &r.CreatedAt)
	if addedBy.Valid {
		r.AddedBy = &addedBy.String
	}
	return r, err
}

// CreateRepo adds a repository.
func (s *Store) CreateRepo(ctx context.Context, owner, name, baseBranch, verifyCommand, testCommand, addedBy string) (Repo, error) {
	if baseBranch == "" {
		baseBranch = "main"
	}
	return scanRepo(s.DB.QueryRowContext(ctx, `
		INSERT INTO repos (owner, name, base_branch, verify_command, test_command, added_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+repoCols, owner, name, baseBranch, verifyCommand, testCommand, addedBy))
}

// ListRepos returns all repos ordered by owner/name.
func (s *Store) ListRepos(ctx context.Context) ([]Repo, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+repoCols+` FROM repos ORDER BY owner, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Repo
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateRepo changes a repo's configuration. Returns ErrNotFound if no row
// matches the given id.
func (s *Store) UpdateRepo(ctx context.Context, id, owner, name, baseBranch, verifyCommand, testCommand string) (Repo, error) {
	if baseBranch == "" {
		baseBranch = "main"
	}
	r, err := scanRepo(s.DB.QueryRowContext(ctx, `
		UPDATE repos SET owner = $2, name = $3, base_branch = $4, verify_command = $5, test_command = $6
		WHERE id = $1
		RETURNING `+repoCols, id, owner, name, baseBranch, verifyCommand, testCommand))
	if errors.Is(err, sql.ErrNoRows) {
		return Repo{}, ErrNotFound
	}
	return r, err
}

// RepoByID resolves a repo by primary key.
func (s *Store) RepoByID(ctx context.Context, id string) (Repo, error) {
	r, err := scanRepo(s.DB.QueryRowContext(ctx, `SELECT `+repoCols+` FROM repos WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Repo{}, ErrNotFound
	}
	return r, err
}

// DeleteRepo removes a repo. Returns ErrNotFound if nothing was deleted.
func (s *Store) DeleteRepo(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM repos WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- jobs --------------------------------------------------------------------

const jobSelect = `
	SELECT j.id, j.repo_id, r.owner || '/' || r.name, j.user_id, u.name,
	       j.feature, j.status, j.engine, j.model, j.editor_model, j.k8s_job_name, j.branch, j.pr_url, j.reason,
	       j.metadata, j.created_at, j.updated_at
	FROM jobs j
	JOIN repos r ON r.id = j.repo_id
	JOIN users u ON u.id = j.user_id`

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var meta []byte
	var model, editorModel sql.NullString
	err := row.Scan(&j.ID, &j.RepoID, &j.RepoName, &j.UserID, &j.UserName,
		&j.Feature, &j.Status, &j.Engine, &model, &editorModel, &j.K8sJobName, &j.Branch, &j.PRURL, &j.Reason,
		&meta, &j.CreatedAt, &j.UpdatedAt)
	j.Model = model.String
	j.EditorModel = editorModel.String
	if len(meta) > 0 && string(meta) != "{}" {
		j.Metadata = json.RawMessage(meta)
	}
	return j, err
}

// CreateJob inserts a new job row in the given initial status. model/editorModel
// record the coding model(s) chosen for the run; an empty string is stored as
// NULL (the engine's deployment default was used).
func (s *Store) CreateJob(ctx context.Context, repoID, userID, feature, status, engine, model, editorModel string) (Job, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO jobs (repo_id, user_id, feature, status, engine, model, editor_model)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		repoID, userID, feature, status, engine, nullIfEmpty(model), nullIfEmpty(editorModel)).Scan(&id)
	if err != nil {
		return Job{}, err
	}
	return s.JobByID(ctx, id)
}

// nullIfEmpty maps "" to a SQL NULL so optional TEXT columns stay NULL rather
// than storing an empty string.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// JobByID resolves a job with its repo/user names joined in.
func (s *Store) JobByID(ctx context.Context, id string) (Job, error) {
	j, err := scanJob(s.DB.QueryRowContext(ctx, jobSelect+` WHERE j.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return j, err
}

// ListJobs returns the most recent jobs, optionally filtered to one user.
func (s *Store) ListJobs(ctx context.Context, userID string, limit int) ([]Job, error) {
	query := jobSelect
	args := []any{}
	if userID != "" {
		query += ` WHERE j.user_id = $1`
		args = append(args, userID)
	}
	query += ` ORDER BY j.created_at DESC LIMIT ` + itoa(limit)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// MarkJobRejected records a job rejected by the harmful-content gate.
func (s *Store) MarkJobRejected(ctx context.Context, id, reason string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE jobs SET status = 'rejected', reason = $2, updated_at = now()
		WHERE id = $1`, id, reason)
	return err
}

// MarkJobRunning records the spawned k8s Job name and branch.
func (s *Store) MarkJobRunning(ctx context.Context, id, jobName, branch string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE jobs SET status = 'running', k8s_job_name = $2, branch = $3, updated_at = now()
		WHERE id = $1`, id, jobName, branch)
	return err
}

// FinishJob records a terminal state (success|failed) with details.
func (s *Store) FinishJob(ctx context.Context, id, status, prURL, reason string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE jobs SET status = $2, pr_url = $3, reason = $4, updated_at = now()
		WHERE id = $1`, id, status, prURL, reason)
	return err
}

// SetJobMetadata stores the config snapshot for a job (JSON object).
func (s *Store) SetJobMetadata(ctx context.Context, id string, metadata json.RawMessage) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE jobs SET metadata = $2 WHERE id = $1`, id, []byte(metadata))
	return err
}

// SetJobLog persists the worker's full log so it survives pod TTL cleanup.
func (s *Store) SetJobLog(ctx context.Context, id, logs string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE jobs SET logs = $2 WHERE id = $1`, id, logs)
	return err
}

// JobLog returns the persisted worker log for a job (empty if none stored yet).
func (s *Store) JobLog(ctx context.Context, id string) (string, error) {
	var logs string
	err := s.DB.QueryRowContext(ctx, `SELECT logs FROM jobs WHERE id = $1`, id).Scan(&logs)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return logs, err
}

// DeleteJob removes a job row. Returns ErrNotFound if nothing was deleted.
func (s *Store) DeleteJob(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM jobs WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RunningJobs returns jobs still in the running state, for the watcher.
func (s *Store) RunningJobs(ctx context.Context) ([]Job, error) {
	rows, err := s.DB.QueryContext(ctx, jobSelect+` WHERE j.status = 'running'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// CountActiveJobs counts jobs currently occupying a worker slot.
func (s *Store) CountActiveJobs(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE status IN ('checking','running')`).Scan(&n)
	return n, err
}
