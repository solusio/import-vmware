package goroutine

// Run runs specified function in goroutine.
// Adds some common panic handling logic like sending report to Sentry.
// Will rethrow occurred panic if any.
func Run(f func()) {
	if f == nil {
		return
	}

	go func() {
		f()
	}()
}
