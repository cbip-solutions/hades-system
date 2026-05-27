package tessera

import "context"

var (
	_ leafAppender   = (*tesseraAppender)(nil)
	_ sthSubscriber  = (*CoSigner)(nil)
	_ CheckpointSink = (*Checkpoint)(nil)
)

// Public methods MUST keep these signatures ( writers
// pin against them). Calling this var initializer is a side-effect-
// free way to get the compiler to type-check every method-value
// expression below; if any signature drifted (e.g. a return type
// changed or a method was renamed) this file fails to compile.
var _ = func() {
	var a *Adapter
	var ctx context.Context
	_ = a.ProjectID
	_ = a.Dir
	_ = a.Config
	_ = a.AppendLeaf
	_ = a.VerifyMerkleInclusion
	_ = a.SubscribeSTH
	_ = a.Close

	var w *Witness
	_ = w.Generate
	_ = w.Load
	_ = w.Sign
	_ = w.PubkeyPEM
	_ = w.Delete

	var cs *CoSigner
	_ = cs.Sign
	_ = cs.OnSTH
	_ = cs.SubscribeAdapter

	var cp *Checkpoint
	_ = cp.Append
	_ = cp.Verify
	_ = cp.Close
	_ = cp.Latest

	var rot *Rotation
	_ = rot.Rotate
	_ = rot.RevokeAndRotate

	var m *Manager
	_ = m.Witness
	_ = m.Checkpoint
	_ = m.CoSigner
	_ = m.ProjectAdapter
	_ = m.Close

	_ = ctx
}
