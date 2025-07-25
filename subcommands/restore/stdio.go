package restore

import (
	"github.com/PlakarKorp/kloset/events"
	"github.com/PlakarKorp/plakar/appcontext"
	"github.com/charmbracelet/lipgloss"
)

var (
	checkMark = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).SetString("✓")
	crossMark = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).SetString("✘")
)

func eventsProcessorStdio(ctx *appcontext.AppContext, quiet bool) chan struct{} {
	done := make(chan struct{})
	go func() {
		for event := range ctx.Events().Listen() {
			switch event := event.(type) {
			case events.PathError:
				ctx.GetLogger().Warn("%x: KO %s %s: %s", event.SnapshotID[:4], crossMark, event.Pathname, event.Message)

			case events.DirectoryError:
				ctx.GetLogger().Warn("%x: KO %s %s: %s", event.SnapshotID[:4], crossMark, event.Pathname, event.Message)

			case events.FileError:
				ctx.GetLogger().Warn("%x: KO %s %s: %s", event.SnapshotID[:4], crossMark, event.Pathname, event.Message)

			case events.DirectoryOK:
				if !quiet {
					ctx.GetLogger().Info("%x: OK %s %s", event.SnapshotID[:4], checkMark, event.Pathname)
				}
			case events.FileOK:
				if !quiet {
					ctx.GetLogger().Info("%x: OK %s %s", event.SnapshotID[:4], checkMark, event.Pathname)
				}
			default:
			}
		}
		done <- struct{}{}
	}()
	return done
}
