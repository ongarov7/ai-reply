package domain

import "errors"

// ErrOutputCharacterLimit means no usable reply was delivered; the existing
// generation reservation is returned to its original quota/credit bucket.
var ErrOutputCharacterLimit = errors.New("AI output character limit exceeded")
