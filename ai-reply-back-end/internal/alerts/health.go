package alerts

import (
	"context"
	"fmt"
	"path/filepath"
	"syscall"
	"time"
)

// Check — мерзімді тексеру: Run қате қайтарса, екі рет қатарынан болған соң
// Severity деңгейінде ескерту, қалпына келгенде RECOVERED.
type Check struct {
	Name     string
	Severity Severity
	Run      func(context.Context) error
}

type checkState struct {
	failing  bool
	failures int
	since    time.Time
}

// Watch — тексерулерді interval аралығымен жүргізеді; контекст жабылғанша.
//
// Two consecutive failures are required before an alert, so one slow query
// does not page anyone; recovery is reported with the downtime. Disabled
// notifier: returns at once.
func (n *Notifier) Watch(ctx context.Context, interval time.Duration, checks ...Check) {
	if !n.Enabled() || len(checks) == 0 {
		return
	}
	if interval <= 0 {
		interval = time.Minute
	}
	states := make([]checkState, len(checks))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for i, c := range checks {
			cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Run(cctx)
			cancel()
			if ctx.Err() != nil {
				return
			}
			st := &states[i]
			if err != nil {
				st.failures++
				if !st.failing && st.failures >= 2 {
					st.failing, st.since = true, n.now()
					n.NotifyTransition(c.Severity, c.Name+" check failing", "check", c.Name, "error", err.Error())
				}
				continue
			}
			if st.failing {
				n.NotifyTransition(Recovered, c.Name+" check recovered", "check", c.Name,
					"downtime", n.now().Sub(st.since).Round(time.Second).String())
			}
			st.failing, st.failures = false, 0
		}
	}
}

// DiskSpace — дерекқор жатқан файлдық жүйеде бос орын minFree-ден төмен болса қате.
func DiskSpace(path string, minFree uint64) func(context.Context) error {
	dir := filepath.Dir(path)
	return func(context.Context) error {
		var st syscall.Statfs_t
		if err := syscall.Statfs(dir, &st); err != nil {
			return fmt.Errorf("statfs %s: %w", dir, err)
		}
		free := uint64(st.Bavail) * uint64(st.Bsize) //nolint:unconvert // Bsize is uint32 on darwin, int64 on linux
		if free < minFree {
			return fmt.Errorf("%s free on %s (below %s)", human(free), dir, human(minFree))
		}
		return nil
	}
}

func human(b uint64) string {
	const unit = 1024
	switch {
	case b >= unit*unit*unit:
		return fmt.Sprintf("%.1f GiB", float64(b)/(unit*unit*unit))
	case b >= unit*unit:
		return fmt.Sprintf("%.0f MiB", float64(b)/(unit*unit))
	default:
		return fmt.Sprintf("%d KiB", b/unit)
	}
}
