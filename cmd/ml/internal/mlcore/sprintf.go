package mlcore

import "fmt"

func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
