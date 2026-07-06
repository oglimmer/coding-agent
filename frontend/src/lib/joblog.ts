// Condensing worker pod logs into a step timeline.
//
// The worker script (worker/run_agent.sh) narrates its pipeline with banner
// lines of the form `=== message ===`: cloning, scoping, aider rounds, self
// review, opening the PR, each review/fix iteration, and the final merge.
// Those banners are the meaningful milestones; everything between them is
// verbose tool output. parseSteps pulls the banners out so the UI can offer a
// "key steps" view alongside the full log.

const BANNER = /^===\s*(.*?)\s*===$/

// stepKey strips a trailing "(...)" detail so a step that the worker re-emits
// as it progresses — e.g. "waiting for review (checks 1/2 …)" then
// "… (checks 2/2 …)" — collapses to a single entry instead of flooding the
// timeline. Distinct milestones (different leading text) stay separate.
function stepKey(text: string): string {
  return text.replace(/\s*\([^)]*\)\s*$/, '')
}

// parseSteps returns the ordered milestone banners in a worker log. Consecutive
// banners that describe the same step keep only the latest (most complete)
// detail line.
export function parseSteps(log: string): string[] {
  const steps: string[] = []
  let lastKey = ''
  for (const raw of log.split('\n')) {
    const m = BANNER.exec(raw.trim())
    if (!m || !m[1]) continue
    const text = m[1]
    const key = stepKey(text)
    if (key === lastKey && steps.length > 0) {
      steps[steps.length - 1] = text // same step, newer detail
    } else {
      steps.push(text)
      lastKey = key
    }
  }
  return steps
}
