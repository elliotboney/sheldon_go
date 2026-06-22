package contracts

// RegionID is the closed set of display regions a compositor snapshot can target
// (AD-6). It is the single source of truth for region ids: the core compositor
// and every renderer compile against these constants, and plugins that claim
// widget regions (Epic 6, AD-14) reference these values — they never mint
// region-id strings.
type RegionID string

// RegionFace is the pet's face region, owned by core. It is the only region at
// M1; widget regions arrive with the plugin registry (Epic 6).
const RegionFace RegionID = "face"

// AllRegions is the closed set of declared regions.
var AllRegions = []RegionID{RegionFace}

// Expression is the render-agnostic mood expression of the face. The mood-drift
// reflex (Story 2.4) sets it from personality state; a renderer maps it to its
// medium. The set is minimal at M1 and extended additively (AD-10).
type Expression string

const (
	// ExpressionNeutral is the resting face.
	ExpressionNeutral Expression = "neutral"
	// ExpressionHappy is a positive-valence face.
	ExpressionHappy Expression = "happy"
	// ExpressionSad is a negative-valence face.
	ExpressionSad Expression = "sad"
)

// Face is the render-agnostic description of the pet's face that any renderer
// maps to its medium — ANSI for the terminal (Story 2.2), an E-Ink bitmap for
// the Waveshare panel (Story 6.1) — from the SAME contract. EyesOpen is what the
// blink reflex (Story 2.3) toggles; Expression is what mood-drift (Story 2.4)
// sets. Defined here, driven later.
type Face struct {
	Expression Expression // mood expression
	EyesOpen   bool       // false during a blink frame
}

// RegionSnapshot is a face-region frame core pushes to the display edge (AD-6).
// Seq is monotonic per region: the renderer renders latest-wins and drops any
// frame whose Seq is not newer than the last rendered (AD-4/NFR12). It is a bus
// payload carried in an Envelope of KindFaceSnapshot.
type RegionSnapshot struct {
	Region RegionID
	Seq    uint64
	Face   Face
}

func (RegionSnapshot) isPayload() {}
