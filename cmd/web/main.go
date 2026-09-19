// Command web is the unified Rewind process (also built as the `rewind` binary):
// web UI, download/ingest/encode workers, in-process SFU, and migrations,
// selected by REWIND_ROLES.
package main

import (
	"os"

	"thirdcoast.systems/rewind/pkg/rewindapp"
)

func main() {
	rewindapp.Run(os.Args[1:])
}
