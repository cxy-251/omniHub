package platform

import "errors"

// ErrInvalidProcessGroup rejects zero/uninitialized ownership objects before
// any process-group or console-wide operation can be attempted.
var ErrInvalidProcessGroup = errors.New("invalid owned process group")
