package email

import (
	"fmt"
	"io"
	"time"
)

func logf(output io.Writer, format string, args ...any) {
	if output == nil {
		return
	}
	fmt.Fprintf(output, "%s mailwatch: %s\n", time.Now().UTC().Format(time.RFC3339Nano), fmt.Sprintf(format, args...))
}
