// Package driveops implements the volume/drive toolbar's right-click
// actions: safely ejecting a removable drive, and opening the OS's native
// disk-management tool for a Format action.
//
// Format deliberately does NOT try to auto-target the specific drive
// clicked: there's no reliable, safe cross-platform way to do that without
// either undocumented platform tricks or real risk of a destructive
// operation landing on the wrong disk. OpenFormatTool just launches the
// native tool (Disk Utility on macOS, Disk Management on Windows) and lets
// the user pick the right drive themselves, the same way they'd already
// trust that tool's own native confirmations.
//
// Eject is lower-risk (unmounting, not destructive) so it does target the
// specific drive — implemented for macOS and Windows only (Linux would
// need resolving a mount point back to its block device first, e.g. via
// udisksctl, and wasn't pursued given Linux is the least-used of the
// three platforms here).
package driveops
