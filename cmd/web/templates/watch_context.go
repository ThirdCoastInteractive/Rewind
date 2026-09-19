package templates

import (
	"fmt"

	"thirdcoast.systems/rewind/internal/db"
)

func contextListStatus(rows []*db.ListContextWindowsForVideoRow) string {
	var windows, shorts int
	for _, row := range rows {
		if row == nil {
			continue
		}
		if row.Kind == "short" {
			shorts++
			continue
		}
		windows++
	}
	if shorts == 0 {
		return fmt.Sprintf("%d context windows", windows)
	}
	return fmt.Sprintf("%d context windows · %d shorts", windows, shorts)
}

func contextShortBelongsTo(sh, parent *db.ListContextWindowsForVideoRow) bool {
	return sh != nil && parent != nil && sh.Kind == "short" && sh.ParentID.Valid && sh.ParentID.Bytes == parent.ID.Bytes
}
