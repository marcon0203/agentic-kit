// Package store holds the sqlc-generated data access code for the AI Agent
// platform. Hand-written SQL lives in queries/; run `make sqlc-gen` after
// editing it. Owner-view and subscriber-view queries are kept physically
// separate (see queries/agents.sql) so the black-box marketplace model
// cannot leak `definition` through a shared code path.
package store
