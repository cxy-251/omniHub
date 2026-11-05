package manga

import "omnihub/internal/archiveutil"

// MaxPages bounds page/chapter indices consistently with the ZIP entry budget.
const MaxPages = archiveutil.MaxEntries
