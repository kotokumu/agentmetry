// Package version defines the coordinated storage generation shared by the
// Atlas target schema and the Go journal-to-projection rebuilder.
package version

// CurrentGeneration is bumped whenever an upgrade requires rebuilding data or
// projections in a new Atlas schema. Older supported generations rebuild
// directly into this generation; intermediate projection migrations are not
// applied.
const CurrentGeneration = 3
