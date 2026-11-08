// Package native runs programs that ship as `runtime` assets on behalf of an
// app (contracts §4, `native.*` bridge commands).
//
// Nothing here knows about any particular program or console. An app hands
// the platform a Spec: which runtime asset to use, the executable inside it,
// arguments, and config files. Arguments and config files are templates:
//
//	{{asset:<id>}}      absolute path of a synced asset (any kind)
//	{{dataDir}}         a writable directory owned by the app (per account)
//	{{virtualPad:<n>}}  whatever the virtual pad resolver says pad n is
//
// Only Go resolves asset ids to paths; apps never see a path, and status and
// error messages carry no paths either.
//
// Runtime archives (.zip or .7z) are extracted once to
// <RuntimesDir>/<name>/<version>/, where name and version come from the
// asset's metadata (`runtime`, `version`). Extraction refuses entries that
// escape the destination and stops at size and file-count limits.
//
// One session runs at a time. A second Launch while a session is preparing,
// running, or stopping fails with ErrBusy. The watchdog waits on the process
// and reports `exited` (clean exit or requested stop) or `crashed` (any
// other exit). Observers are told when a session starts and ends, which is
// how the input hub pushes and pops its focus-stack provider without this
// package importing it.
package native
