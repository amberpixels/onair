// Package onair answers one question - which commit is actually running - by
// comparing three truth tiers: HEAD (latest commit on the tracked branch),
// Green (latest commit whose CI pipeline passed) and Live (what the running
// artifact reports about itself).
//
// The core is pure: it asks abstract questions through three seams - Forge
// (git + CI truth), LiveProvider (what is running, per component) and Identity
// (who is viewing, for attribution) - and returns a Report. Rendering lives in
// package render, concrete providers in packages gitlab and live, and wiring
// in cmd/onair. Host applications import this package directly, wire their own
// providers and render the Report however they like.
package onair
