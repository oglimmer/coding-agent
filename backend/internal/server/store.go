package server

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrNotFound is returned by Store lookups when no row matches.
var ErrNotFound = errors.New("not found")

// User is a platform account.
type User struct {
	ID           string    `json:"id"`
	OIDCSub      string    `json:"-"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	IsAdmin      bool      `json:"isAdmin"`
	TokenVersion int       `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Repo is a GitHub repository users can target.
type Repo struct {
	ID         string    `json:"id"`
	Owner      string    `json:"owner"`
	Name       string    `json:"name"`
	BaseBranch string    `json:"baseBranch"`
	AddedBy    *string   `json:"addedBy,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// FullName returns "owner/name".
func (r Repo) FullName() string { return r.Owner + "/" + r.Name }

// Job is one feature request and its lifecycle.
type Job struct {
	ID         string    `json:"id"`
	RepoID     string    `json:"repoId"`
	RepoName   string    `json:"repoName"`
	UserID     string    `json:"userId"`
	UserName   string    `json:"userName"`
	Feature    string    `json:"feature"`
	Status     string    `json:"status"`
	K8sJobName string    `json:"-"`
	Branch     string    `json:"branch,omitempty"`
	PRURL      string    `json:"prUrl,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
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
	firstUser := count == 0

	var u User
	err = tx.QueryRowContext(ctx, `
		INSERT INTO users (oidc_sub, email, name, is_admin)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (oidc_sub) DO UPDATE
		  SET email = EXCLUDED.email, name = EXCLUDED.name
		RETURNING id, oidc_sub, email, name, is_admin, token_version, created_at`,
		sub, email, name, firstUser,
	).Scan(&u.ID, &u.OIDCSub, &u.Email, &u.Name, &u.IsAdmin, &u.TokenVersion, &u.CreatedAt)
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
	err := row.Scan(&u.ID, &sub, &u.Email, &u.Name, &u.IsAdmin, &u.TokenVersion, &u.CreatedAt)
	u.OIDCSub = sub.String
	return u, err
}

const userCols = `id, oidc_sub, email, name, is_admin, token_version, created_at`

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
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE is_admin`).Scan(&n)
	return n, err
}

// SetAdmin grants or revokes admin and bumps token_version so existing sessions
// re-resolve their privileges. Returns the updated user.
func (s *Store) SetAdmin(ctx context.Context, id string, admin bool) (User, error) {
	u, err := scanUser(s.DB.QueryRowContext(ctx, `
		UPDATE users SET is_admin = $2, token_version = token_version + 1
		WHERE id = $1
		RETURNING `+userCols, id, admin))
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

const repoCols = `id, owner, name, base_branch, added_by, created_at`

func scanRepo(row interface{ Scan(...any) error }) (Repo, error) {
	var r Repo
	var addedBy sql.NullString
	err := row.Scan(&r.ID, &r.Owner, &r.Name, &r.BaseBranch, &addedBy, &r.CreatedAt)
	if addedBy.Valid {
		r.AddedBy = &addedBy.String
	}
	return r, err
}

// CreateRepo adds a repository.
func (s *Store) CreateRepo(ctx context.Context, owner, name, baseBranch, addedBy string) (Repo, error) {
	if baseBranch == "" {
		baseBranch = "main"
	}
	return scanRepo(s.DB.QueryRowContext(ctx, `
		INSERT INTO repos (owner, name, base_branch, added_by)
		VALUES ($1, $2, $3, $4)
		RETURNING `+repoCols, owner, name, baseBranch, addedBy))
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
	       j.feature, j.status, j.k8s_job_name, j.branch, j.pr_url, j.reason,
	       j.created_at, j.updated_at
	FROM jobs j
	JOIN repos r ON r.id = j.repo_id
	JOIN users u ON u.id = j.user_id`

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.RepoID, &j.RepoName, &j.UserID, &j.UserName,
		&j.Feature, &j.Status, &j.K8sJobName, &j.Branch, &j.PRURL, &j.Reason,
		&j.CreatedAt, &j.UpdatedAt)
	return j, err
}

// CreateJob inserts a new job row in the given initial status.
func (s *Store) CreateJob(ctx context.Context, repoID, userID, feature, status string) (Job, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO jobs (repo_id, user_id, feature, status)
		VALUES ($1, $2, $3, $4) RETURNING id`, repoID, userID, feature, status).Scan(&id)
	if err != nil {
		return Job{}, err
	}
	return s.JobByID(ctx, id)
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
