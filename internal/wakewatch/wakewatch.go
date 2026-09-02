// Package wakewatch detects the OS waking from sleep and calls back into
// the app so it can proactively nudge Fyne's GLFW-driven event loop.
// Community reports of Fyne's known "window becomes unresponsive after
// sitting in the background a while" bug (fyne-io/fyne#2791, #2536) say a
// manual resize or focus change sometimes "wakes" a stalled window; this
// lets the app try the same nudge automatically right as the OS reports
// waking, instead of waiting for the user to notice and do it by hand.
// This is a best-effort mitigation for an upstream bug, not a fix — see
// CLAUDE.md / project memory "Intermittent freeze report" for the full
// investigation. Currently implemented for macOS only
// (NSWorkspaceDidWakeNotification via a small Cocoa bridge); Windows/Linux
// are no-ops for now — Supported() reports false and Install does nothing.
package wakewatch
