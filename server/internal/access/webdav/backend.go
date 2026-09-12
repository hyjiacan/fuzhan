package webdav

// Backend declares the service-layer capabilities WebDAV delegates to the Web
// upload/index/audit system, so WebDAV cannot act as a bypass of the upload
// switch, extension whitelist, disk/quota checks, or the index/hash chain.
//
// A nil Backend keeps the legacy permissive behavior used by unit tests; in the
// running server the implementation is injected by router.go.
type Backend interface {
	// UploadEnabled reports the global public-upload master switch (H3).
	UploadEnabled() bool

	// IsAdmin reports whether the user (by UUID) has the admin role.
	IsAdmin(userUUID string) bool

	// CheckDiskSpace verifies the partition hosting path has room for required.
	CheckDiskSpace(path string, required int64) error

	// CheckPrivateQuota verifies a user's private-storage quota (<= 0 = unlimited).
	CheckPrivateQuota(userUUID string, addSize int64) error

	// ValidateExtension checks a filename against the allowed-extensions whitelist.
	// Empty whitelist allows everything; returns error for disallowed types.
	ValidateExtension(filename string) error

	// SyncPublicFile writes a public file into the index, updates uploader IP,
	// marks it recently-synced, and triggers the hash chain (H2).
	SyncPublicFile(rootName, relPath, uploaderIP string)

	// RemovePublicFile soft-deletes a public file's index record.
	RemovePublicFile(rootName, relPath string)

	// MovePublicFile syncs a public file's index to its new relative path.
	MovePublicFile(rootName, oldRel, newRel string)

	// AddPrivateFile writes a private file record (with share code) and triggers
	// the hash chain; performs the real-size quota check (returns error on quota
	// violation, caller removes the on-disk file).
	AddPrivateFile(userUUID, relPath, fileName string, size int64) error

	// SoftDeletePrivateFile soft-deletes a user's private file record.
	SoftDeletePrivateFile(userUUID, relPath string) error

	// RenamePrivateFile syncs a user's private file record to its new path.
	RenamePrivateFile(userUUID, oldRel, newRel string) error
}
