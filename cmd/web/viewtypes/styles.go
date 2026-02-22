package viewtypes

// ============================================================================
// SHARED CSS CLASS CONSTANTS
// Reusable Tailwind class strings used across multiple template files.
// Use these with class={ VarName } or class={ VarName + " extra-classes" }.
// ============================================================================

// SectionLabel is the standard label style for form sections, panel headings, etc.
// Add spacing (mb-1, mb-2, px-3 py-1) per use site as needed.
var SectionLabel = "text-xs text-white/40 font-mono uppercase tracking-wider"

// GhostButtonSm is a small ghost-style button (outlined, no fill).
var GhostButtonSm = "px-3 py-1 text-xs font-mono uppercase tracking-wider transition-all border-2 bg-black text-white border-white/20 hover:border-white/40 active:scale-95"

// PageHeading is the main h1 heading style for top-level pages.
var PageHeading = "text-2xl font-mono font-bold uppercase tracking-wider"

// SubHeading is for secondary headings (h2 level) within pages.
var SubHeading = "text-lg font-mono font-bold uppercase tracking-wider"

// InfoBoxClass is the standard info/detail panel container.
var InfoBoxClass = "bg-black border-2 border-white/10 p-3"

// InputClass is the standard text input styling.
var InputClass = "w-full px-3 py-2 bg-black border-2 border-white/20 text-white placeholder-white/40 focus:border-white transition font-mono text-sm"
