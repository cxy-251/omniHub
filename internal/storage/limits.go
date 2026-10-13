package storage

import "errors"

// MaxImageReadBytes bounds a single returned image BLOB byte buffer.
const MaxImageReadBytes = 32 << 20

// MaxThumbnailReadBytes bounds a hot thumbnail BLOB response buffer.
const MaxThumbnailReadBytes = 2 << 20

// ErrBlobCapacity reports image data beyond its read budget, before loading it.
var ErrBlobCapacity = errors.New("image exceeds read capacity")
