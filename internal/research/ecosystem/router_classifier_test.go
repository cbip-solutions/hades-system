package ecosystem

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogisticClassifier_TrainAndPredict(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	trainSet := generateLabeledCorpus(rng, 1000)
	validSet := generateLabeledCorpus(rng, 200)

	classifier := NewLogisticClassifier(LogisticClassifierConfig{
		EmbeddingDim: 1536,
		Ecosystems:   AllEcosystems,
		LearningRate: 0.05,
		Epochs:       30,
		L2:           1e-4,
		Seed:         42,
	})

	if err := classifier.Fit(context.Background(), trainSet); err != nil {
		t.Fatalf("Fit: %v", err)
	}

	var correct int
	for _, sample := range validSet {
		softmax, err := classifier.ScoreSoftmax(context.Background(), sample.Embedding)
		if err != nil {
			t.Fatalf("ScoreSoftmax: %v", err)
		}
		if argmax(softmax) == sample.Label {
			correct++
		}
	}
	accuracy := float64(correct) / float64(len(validSet))
	if accuracy < 0.70 {
		t.Errorf("classifier accuracy %.3f < 0.70 floor on synthetic corpus", accuracy)
	}
}

func TestLogisticClassifier_ScoreSoftmaxIsValid(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{
		EmbeddingDim: 1536, Ecosystems: AllEcosystems, Epochs: 5, Seed: 7,
	})
	rng := rand.New(rand.NewSource(7))
	if err := cls.Fit(context.Background(), generateLabeledCorpus(rng, 200)); err != nil {
		t.Fatalf("Fit: %v", err)
	}
	for i := 0; i < 20; i++ {
		emb := randomEmbedding(rng)
		s, err := cls.ScoreSoftmax(context.Background(), emb)
		if err != nil {
			t.Fatalf("ScoreSoftmax[%d]: %v", i, err)
		}
		if err := validateSoftmax(s); err != nil {
			t.Errorf("ScoreSoftmax[%d] violates router contract: %v (got %v)", i, err, s)
		}
	}
}

func TestLogisticClassifier_ImplementsQueryClassifier(t *testing.T) {
	var _ QueryClassifier = (*LogisticClassifier)(nil)
}

func TestLogisticClassifier_CheckpointSerializeDeserialize(t *testing.T) {
	classifier := NewLogisticClassifier(LogisticClassifierConfig{
		EmbeddingDim: 1536, Ecosystems: AllEcosystems, LearningRate: 0.05, Epochs: 3, Seed: 7,
	})
	rng := rand.New(rand.NewSource(7))
	trainSet := generateLabeledCorpus(rng, 200)
	if err := classifier.Fit(context.Background(), trainSet); err != nil {
		t.Fatalf("Fit: %v", err)
	}

	var buf bytes.Buffer
	if err := classifier.WriteCheckpoint(&buf); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	restored := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	if err := restored.ReadCheckpoint(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("ReadCheckpoint: %v", err)
	}

	if classifier.CheckpointHash() != restored.CheckpointHash() {
		t.Errorf("checkpoint hash mismatch after roundtrip: %s vs %s",
			classifier.CheckpointHash(), restored.CheckpointHash())
	}
	if classifier.CheckpointHash() == "" {
		t.Errorf("post-Fit CheckpointHash unexpectedly empty")
	}

	probe := rand.New(rand.NewSource(99))
	for i := 0; i < 50; i++ {
		emb := randomEmbedding(probe)
		sA, errA := classifier.ScoreSoftmax(context.Background(), emb)
		sB, errB := restored.ScoreSoftmax(context.Background(), emb)
		if errA != nil || errB != nil {
			t.Fatalf("ScoreSoftmax errA=%v errB=%v", errA, errB)
		}
		for _, e := range AllEcosystems {
			if absDiff(sA[e], sB[e]) > 1e-9 {
				t.Fatalf("prediction divergence after roundtrip on %s: %v vs %v", e, sA[e], sB[e])
			}
		}
	}
}

func TestLogisticClassifier_CheckpointRejectsBadMagic(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	err := cls.ReadCheckpoint(bytes.NewReader([]byte("XXXX")))
	if err == nil {
		t.Errorf("expected error on bad magic; got nil")
	}
	if !strings.Contains(err.Error(), "magic") {
		t.Errorf("expected 'magic' in error, got: %v", err)
	}
}

func TestLogisticClassifier_CheckpointRejectsTruncated(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	rng := rand.New(rand.NewSource(1))
	if err := cls.Fit(context.Background(), generateLabeledCorpus(rng, 100)); err != nil {
		t.Fatalf("Fit: %v", err)
	}
	var buf bytes.Buffer
	if err := cls.WriteCheckpoint(&buf); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	full := buf.Bytes()
	half := full[:len(full)/2]

	restored := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	err := restored.ReadCheckpoint(bytes.NewReader(half))
	if err == nil {
		t.Errorf("expected error on truncated checkpoint; got nil")
	}
}

func TestLogisticClassifier_CheckpointShapeMismatch(t *testing.T) {
	src := NewLogisticClassifier(LogisticClassifierConfig{
		EmbeddingDim: 1536, Ecosystems: AllEcosystems, Epochs: 2, Seed: 3,
	})
	rng := rand.New(rand.NewSource(3))
	_ = src.Fit(context.Background(), generateLabeledCorpus(rng, 100))
	var buf bytes.Buffer
	if err := src.WriteCheckpoint(&buf); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}
	dst := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 256, Ecosystems: AllEcosystems})
	err := dst.ReadCheckpoint(bytes.NewReader(buf.Bytes()))
	if err == nil {
		t.Errorf("expected error on shape mismatch (1536→256); got nil")
	}
	if !strings.Contains(err.Error(), "shape") {
		t.Errorf("expected 'shape' in error, got: %v", err)
	}
}

func TestLogisticClassifier_PreFitHashIsEmpty(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	if h := cls.CheckpointHash(); h != "" {
		t.Errorf("pre-Fit CheckpointHash should be empty, got %q", h)
	}
}

func TestLogisticClassifier_DeterministicTraining(t *testing.T) {
	makeCorpus := func() []LabeledSample {
		rng := rand.New(rand.NewSource(101))
		return generateLabeledCorpus(rng, 300)
	}
	cls1 := NewLogisticClassifier(LogisticClassifierConfig{
		EmbeddingDim: 1536, Ecosystems: AllEcosystems, Epochs: 5, Seed: 555,
	})
	cls2 := NewLogisticClassifier(LogisticClassifierConfig{
		EmbeddingDim: 1536, Ecosystems: AllEcosystems, Epochs: 5, Seed: 555,
	})
	if err := cls1.Fit(context.Background(), makeCorpus()); err != nil {
		t.Fatalf("Fit cls1: %v", err)
	}
	if err := cls2.Fit(context.Background(), makeCorpus()); err != nil {
		t.Fatalf("Fit cls2: %v", err)
	}
	if cls1.CheckpointHash() != cls2.CheckpointHash() {
		t.Errorf("non-deterministic training: hash1=%s hash2=%s",
			cls1.CheckpointHash(), cls2.CheckpointHash())
	}
}

func TestRetrain_RejectsInvalidEmbeddingDim(t *testing.T) {
	classifier := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	bad := []LabeledSample{{Embedding: make([]float32, 100), Label: EcoGo}}
	err := classifier.Fit(context.Background(), bad)
	if err == nil {
		t.Errorf("expected error on embedding dim mismatch; got nil")
	}
}

func TestLogisticClassifier_FitRejectsEmpty(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	err := cls.Fit(context.Background(), nil)
	if err == nil {
		t.Errorf("expected error on empty training set; got nil")
	}
}

func TestLogisticClassifier_FitContextCancel(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{
		EmbeddingDim: 1536, Ecosystems: AllEcosystems, Epochs: 1000, Seed: 9,
	})
	rng := rand.New(rand.NewSource(9))
	corpus := generateLabeledCorpus(rng, 500)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := cls.Fit(ctx, corpus)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestLogisticClassifier_ScoreSoftmaxContextCancel(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := cls.ScoreSoftmax(ctx, make([]float32, 1536))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestLogisticClassifier_ScoreSoftmaxDimMismatch(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	_, err := cls.ScoreSoftmax(context.Background(), make([]float32, 100))
	if err == nil {
		t.Errorf("expected dim-mismatch error; got nil")
	}
}

func TestLogisticClassifier_ScoreSoftmaxNaNGuard(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	emb := make([]float32, 1536)
	emb[0] = float32(math.NaN())
	emb[1] = float32(math.Inf(1))
	s, err := cls.ScoreSoftmax(context.Background(), emb)
	if err != nil {

		return
	}
	// If accepted, output MUST still be finite (no NaN leak past boundary).
	for k, v := range s {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("NaN/Inf leak in softmax[%s]=%v", k, v)
		}
	}
}

func TestRetrainCLI_PersistsAndReloads(t *testing.T) {
	tmp := t.TempDir()
	ckptPath := filepath.Join(tmp, "classifier.bin")

	err := RetrainAndPersist(context.Background(), RetrainOptions{
		BootstrapCorpusPath: "",
		OutputPath:          ckptPath,
		Seed:                123,
		Epochs:              5,
	})
	if err != nil {
		t.Fatalf("RetrainAndPersist: %v", err)
	}
	info, err := os.Stat(ckptPath)
	if err != nil {
		t.Fatalf("expected checkpoint at %s: %v", ckptPath, err)
	}
	if info.Size() == 0 {
		t.Errorf("checkpoint file is empty")
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("checkpoint mode %o != 0o600", perm)
	}

	loaded, err := LoadLogisticClassifier(ckptPath)
	if err != nil {
		t.Fatalf("LoadLogisticClassifier: %v", err)
	}
	if loaded.CheckpointHash() == "" {
		t.Errorf("loaded classifier has empty CheckpointHash")
	}
}

func TestRetrainCLI_AtomicRenameOnFailure(t *testing.T) {
	tmp := t.TempDir()

	bogus := filepath.Join(tmp, "nope", "classifier.bin")

	err := RetrainAndPersist(context.Background(), RetrainOptions{
		OutputPath: bogus,
		Seed:       1,
		Epochs:     1,
	})
	if err == nil {
		t.Errorf("expected error on non-existent output dir; got nil")
	}

	if _, statErr := os.Stat(bogus); !os.IsNotExist(statErr) {
		t.Errorf("expected canonical path absent on failure, got stat=%v", statErr)
	}
}

func TestLoadLogisticClassifier_MissingFile(t *testing.T) {
	_, err := LoadLogisticClassifier(filepath.Join(t.TempDir(), "does-not-exist.bin"))
	if err == nil {
		t.Errorf("expected error on missing file; got nil")
	}
}

func TestLoadLogisticClassifier_CorruptFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "corrupt.bin")
	if err := os.WriteFile(path, []byte("not a checkpoint file"), 0o600); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}
	_, err := LoadLogisticClassifier(path)
	if err == nil {
		t.Errorf("expected error on corrupt file; got nil")
	}
}

func TestRetrainAndPersist_PropagatesFitError(t *testing.T) {
	tmp := t.TempDir()
	corpusPath := filepath.Join(tmp, "tiny.tsv")

	if err := os.WriteFile(corpusPath, []byte("# header\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	err := RetrainAndPersist(context.Background(), RetrainOptions{
		BootstrapCorpusPath: corpusPath,
		OutputPath:          filepath.Join(tmp, "out.bin"),
		Seed:                1,
		Epochs:              1,
	})
	if err == nil {
		t.Errorf("expected error from RetrainAndPersist on empty corpus; got nil")
	}
}

func TestNewLogisticClassifier_PreservesNonDefaults(t *testing.T) {
	cfg := LogisticClassifierConfig{
		EmbeddingDim: 256,
		Ecosystems:   []Ecosystem{EcoGo, EcoPython},
		LearningRate: 0.01,
		Epochs:       5,
		BatchSize:    16,
		L2:           1e-3,
		Seed:         1,
	}
	cls := NewLogisticClassifier(cfg)
	if cls.cfg.EmbeddingDim != 256 {
		t.Errorf("EmbeddingDim clobbered: %d", cls.cfg.EmbeddingDim)
	}
	if cls.cfg.LearningRate != 0.01 {
		t.Errorf("LearningRate clobbered: %g", cls.cfg.LearningRate)
	}
	if cls.cfg.Epochs != 5 {
		t.Errorf("Epochs clobbered: %d", cls.cfg.Epochs)
	}
	if cls.cfg.BatchSize != 16 {
		t.Errorf("BatchSize clobbered: %d", cls.cfg.BatchSize)
	}
	if cls.cfg.L2 != 1e-3 {
		t.Errorf("L2 clobbered: %g", cls.cfg.L2)
	}
	if len(cls.weights) != 2 || len(cls.weights[0]) != 256 {
		t.Errorf("shape: weights=%dx%d (want 2x256)", len(cls.weights), len(cls.weights[0]))
	}
}

func TestNewLogisticClassifier_AppliesAllDefaults(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{})
	if cls.cfg.EmbeddingDim != classifierEmbeddingDim {
		t.Errorf("EmbeddingDim default missing: %d", cls.cfg.EmbeddingDim)
	}
	if len(cls.cfg.Ecosystems) != len(AllEcosystems) {
		t.Errorf("Ecosystems default missing: %v", cls.cfg.Ecosystems)
	}
	if cls.cfg.LearningRate != 0.05 {
		t.Errorf("LearningRate default missing: %g", cls.cfg.LearningRate)
	}
	if cls.cfg.Epochs != 30 {
		t.Errorf("Epochs default missing: %d", cls.cfg.Epochs)
	}
	if cls.cfg.BatchSize != 64 {
		t.Errorf("BatchSize default missing: %d", cls.cfg.BatchSize)
	}
	if cls.cfg.L2 != 1e-4 {
		t.Errorf("L2 default missing: %g", cls.cfg.L2)
	}
}

func TestLoadBootstrapCorpus_TSV(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "corpus.tsv")

	var buf bytes.Buffer
	rng := rand.New(rand.NewSource(7))
	for i, eco := range AllEcosystems {
		for j := 0; j < 1536; j++ {
			if j > 0 {
				buf.WriteByte(' ')
			}
			fmt.Fprintf(&buf, "%g", rng.NormFloat64()+float64(i))
		}
		buf.WriteByte('\t')
		buf.WriteString(string(eco))
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	samples, err := loadBootstrapCorpus(path)
	if err != nil {
		t.Fatalf("loadBootstrapCorpus: %v", err)
	}
	if len(samples) != 4 {
		t.Fatalf("expected 4 samples, got %d", len(samples))
	}
	for i, s := range samples {
		if len(s.Embedding) != 1536 {
			t.Errorf("row %d: embedding len %d != 1536", i, len(s.Embedding))
		}
		if s.Label != AllEcosystems[i] {
			t.Errorf("row %d: label %q != %q", i, s.Label, AllEcosystems[i])
		}
	}
}

func TestLoadBootstrapCorpus_RejectsBadLabel(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad.tsv")
	var buf bytes.Buffer
	for j := 0; j < 1536; j++ {
		if j > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString("0")
	}
	buf.WriteString("\thaskell\n")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := loadBootstrapCorpus(path)
	if err == nil {
		t.Errorf("expected error on unknown ecosystem label; got nil")
	}
}

func TestLoadBootstrapCorpus_RejectsUnparseableFloat(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad-float.tsv")
	var buf bytes.Buffer
	for j := 0; j < 1535; j++ {
		if j > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString("0")
	}
	buf.WriteString(" not-a-float\tgo\n")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := loadBootstrapCorpus(path)
	if err == nil {
		t.Errorf("expected error on unparseable float; got nil")
	}
}

func TestLoadBootstrapCorpus_RejectsMissingTab(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "no-tab.tsv")
	if err := os.WriteFile(path, []byte("0 0 0 go\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := loadBootstrapCorpus(path)
	if err == nil {
		t.Errorf("expected error on missing tab; got nil")
	}
	if !strings.Contains(err.Error(), "tab") {
		t.Errorf("expected 'tab' in error, got: %v", err)
	}
}

func TestLoadBootstrapCorpus_EmptyAndComments(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "comments-only.tsv")
	if err := os.WriteFile(path, []byte("# header line\n\n# another comment\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := loadBootstrapCorpus(path)
	if err == nil {
		t.Errorf("expected error on empty corpus (after comment filter); got nil")
	}
	if !strings.Contains(err.Error(), "no samples") {
		t.Errorf("expected 'no samples' in error, got: %v", err)
	}
}

func TestLoadBootstrapCorpus_MissingFile(t *testing.T) {
	_, err := loadBootstrapCorpus(filepath.Join(t.TempDir(), "nope.tsv"))
	if err == nil {
		t.Errorf("expected error on missing file; got nil")
	}
}

func TestReadCheckpoint_RejectsUnsupportedVersion(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	rng := rand.New(rand.NewSource(1))
	_ = cls.Fit(context.Background(), generateLabeledCorpus(rng, 100))
	var buf bytes.Buffer
	if err := cls.WriteCheckpoint(&buf); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}
	b := buf.Bytes()

	b[4] = 99
	dst := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	err := dst.ReadCheckpoint(bytes.NewReader(b))
	if err == nil {
		t.Errorf("expected error on unsupported version; got nil")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("expected 'version' in error, got: %v", err)
	}
}

func TestReadCheckpoint_RejectsTamperedWeights(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	rng := rand.New(rand.NewSource(1))
	_ = cls.Fit(context.Background(), generateLabeledCorpus(rng, 100))
	var buf bytes.Buffer
	if err := cls.WriteCheckpoint(&buf); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}
	b := buf.Bytes()

	headerLen := len(classifierMagic) + 1 + 1 + 4
	b[headerLen] ^= 0xFF
	dst := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	err := dst.ReadCheckpoint(bytes.NewReader(b))
	if err == nil {
		t.Errorf("expected error on tampered weights; got nil")
	}
	if !strings.Contains(err.Error(), "integrity") {
		t.Errorf("expected 'integrity' in error, got: %v", err)
	}
}

func TestRetrainAndPersist_RejectsEmptyOutputPath(t *testing.T) {
	err := RetrainAndPersist(context.Background(), RetrainOptions{
		OutputPath: "",
		Seed:       1,
		Epochs:     1,
	})
	if err == nil {
		t.Errorf("expected error on empty OutputPath; got nil")
	}
}

func TestWriteCheckpoint_PropagatesWriteFailure(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	rng := rand.New(rand.NewSource(1))
	_ = cls.Fit(context.Background(), generateLabeledCorpus(rng, 100))
	if err := cls.WriteCheckpoint(failingWriter{}); err == nil {
		t.Errorf("expected error from failing writer; got nil")
	}
}

func TestWriteCheckpoint_PropagatesPartialWriteFailure(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	rng := rand.New(rand.NewSource(1))
	_ = cls.Fit(context.Background(), generateLabeledCorpus(rng, 100))

	cutoffs := []int{4, 5, 6, 11, 10 + 4*4*1536, 10 + 4*4*1536 + 16 - 4}
	for _, after := range cutoffs {
		err := cls.WriteCheckpoint(&truncatingWriter{after: after})
		if err == nil {
			t.Errorf("expected error from truncating writer (cutoff=%d); got nil", after)
		}
	}
}

func TestReadCheckpoint_PropagatesPartialReadFailure(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
	rng := rand.New(rand.NewSource(1))
	_ = cls.Fit(context.Background(), generateLabeledCorpus(rng, 100))
	var buf bytes.Buffer
	if err := cls.WriteCheckpoint(&buf); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}
	full := buf.Bytes()

	cutoffs := []int{15, 10 + 4*4*1536 + 4, 10 + 4*4*1536 + 16 + 4}
	for _, sz := range cutoffs {
		if sz > len(full) {
			continue
		}
		dst := NewLogisticClassifier(LogisticClassifierConfig{EmbeddingDim: 1536, Ecosystems: AllEcosystems})
		if err := dst.ReadCheckpoint(bytes.NewReader(full[:sz])); err == nil {
			t.Errorf("expected error from truncated read (cutoff=%d); got nil", sz)
		}
	}
}

func TestLoadBootstrapCorpus_RejectsWrongDim(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "shortdim.tsv")
	var buf bytes.Buffer
	for j := 0; j < 100; j++ {
		if j > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString("0")
	}
	buf.WriteString("\tgo\n")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := loadBootstrapCorpus(path)
	if err == nil {
		t.Errorf("expected error on wrong-dim embedding; got nil")
	}
}

func TestRetrainAndPersist_FromTSV(t *testing.T) {
	tmp := t.TempDir()
	corpusPath := filepath.Join(tmp, "corpus.tsv")
	ckptPath := filepath.Join(tmp, "classifier.bin")

	var buf bytes.Buffer
	rng := rand.New(rand.NewSource(31))
	means := map[Ecosystem]float64{EcoGo: 1.0, EcoPython: 2.0, EcoTypeScript: 3.0, EcoRust: 4.0}
	for i := 0; i < 40; i++ {
		eco := AllEcosystems[i%len(AllEcosystems)]
		for j := 0; j < 1536; j++ {
			if j > 0 {
				buf.WriteByte(' ')
			}
			fmt.Fprintf(&buf, "%g", means[eco]+rng.NormFloat64()*0.5)
		}
		buf.WriteByte('\t')
		buf.WriteString(string(eco))
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(corpusPath, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write corpus: %v", err)
	}

	err := RetrainAndPersist(context.Background(), RetrainOptions{
		BootstrapCorpusPath: corpusPath,
		OutputPath:          ckptPath,
		Seed:                41,
		Epochs:              5,
	})
	if err != nil {
		t.Fatalf("RetrainAndPersist[TSV]: %v", err)
	}
	if _, err := os.Stat(ckptPath); err != nil {
		t.Fatalf("expected ckpt at %s: %v", ckptPath, err)
	}
}

func TestLogisticClassifier_RouterIntegration(t *testing.T) {
	cls := NewLogisticClassifier(LogisticClassifierConfig{
		EmbeddingDim: 1536, Ecosystems: AllEcosystems, Epochs: 20, Seed: 11,
	})
	rng := rand.New(rand.NewSource(11))
	if err := cls.Fit(context.Background(), generateLabeledCorpus(rng, 500)); err != nil {
		t.Fatalf("Fit: %v", err)
	}
	router, err := NewRouter(RouterConfig{
		Heuristics:      defaultHeuristics(),
		Classifier:      cls,
		MarginBroadcast: 0.10,
		MarginTop2:      0.20,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	embGo := randomEmbeddingForEcosystem(rng, EcoGo)
	dec, err := router.Classify(context.Background(), "what is the best way to handle errors", embGo)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if dec.Ecosystems[0] != EcoGo {
		t.Errorf("expected top-1=EcoGo, got %v (full=%v)", dec.Ecosystems[0], dec.Ecosystems)
	}
	if router.ClassifierCheckpointHash() == "" {
		t.Errorf("router exposes empty checkpoint hash after Fit")
	}
}

func generateLabeledCorpus(rng *rand.Rand, n int) []LabeledSample {
	out := make([]LabeledSample, n)
	for i := 0; i < n; i++ {
		eco := AllEcosystems[i%len(AllEcosystems)]
		emb := randomEmbeddingForEcosystem(rng, eco)
		out[i] = LabeledSample{Embedding: emb, Label: eco}
	}
	return out
}

func randomEmbedding(rng *rand.Rand) []float32 {
	e := make([]float32, 1536)
	for i := range e {
		e[i] = float32(rng.NormFloat64())
	}
	return e
}

func randomEmbeddingForEcosystem(rng *rand.Rand, eco Ecosystem) []float32 {

	return syntheticEmbedding(rng, eco)
}

func argmax(softmax map[Ecosystem]float64) Ecosystem {
	var best Ecosystem
	bestV := -1.0
	for _, e := range AllEcosystems {
		if softmax[e] > bestV {
			bestV = softmax[e]
			best = e
		}
	}
	return best
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("simulated I/O failure")
}

type truncatingWriter struct {
	after   int
	written int
}

func (w *truncatingWriter) Write(p []byte) (int, error) {
	if w.written >= w.after {
		return 0, errors.New("simulated truncation")
	}
	remaining := w.after - w.written
	if len(p) <= remaining {
		w.written += len(p)
		return len(p), nil
	}
	w.written = w.after
	return remaining, errors.New("simulated truncation (partial)")
}

func readAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}

type recordingSink struct {
	buf       bytes.Buffer
	syncErr   error
	closeErr  error
	syncCalls int
	closed    bool
}

func (s *recordingSink) Write(p []byte) (int, error) { return s.buf.Write(p) }
func (s *recordingSink) Sync() error                 { s.syncCalls++; return s.syncErr }
func (s *recordingSink) Close() error                { s.closed = true; return s.closeErr }

func makeFailingFactory(syncErr, closeErr error, tmpRef *string) fileSinkFactory {
	return func(path string) (fileSink, string, error) {
		tmp := path + ".tmp"
		if tmpRef != nil {
			*tmpRef = tmp
		}
		return &recordingSink{syncErr: syncErr, closeErr: closeErr}, tmp, nil
	}
}

func recordingRemove(seen *[]string) removeFunc {
	return func(path string) error {
		*seen = append(*seen, path)

		return os.Remove(path)
	}
}

func TestRetrainAndPersist_FsyncFailure(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "fsync-fail.bin")
	var tmpSeen string
	var removed []string

	deps := retrainPersistDeps{
		openSink: makeFailingFactory(errors.New("simulated fsync"), nil, &tmpSeen),
		rename:   os.Rename,
		remove:   recordingRemove(&removed),
	}
	err := retrainAndPersistWithDeps(context.Background(), RetrainOptions{
		OutputPath: out,
		Seed:       1,
		Epochs:     1,
	}, deps)
	if err == nil {
		t.Fatalf("expected fsync error; got nil")
	}
	if !strings.Contains(err.Error(), "fsync") {
		t.Errorf("expected 'fsync' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "simulated fsync") {
		t.Errorf("expected wrapped underlying fsync cause, got: %v", err)
	}

	if len(removed) != 1 || removed[0] != tmpSeen {
		t.Errorf("expected remove(%q), got %v", tmpSeen, removed)
	}

	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("expected canonical path absent on fsync failure, got stat=%v", statErr)
	}
}

func TestRetrainAndPersist_CloseFailure(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "close-fail.bin")
	var tmpSeen string
	var removed []string

	deps := retrainPersistDeps{
		openSink: makeFailingFactory(nil, errors.New("simulated close"), &tmpSeen),
		rename:   os.Rename,
		remove:   recordingRemove(&removed),
	}
	err := retrainAndPersistWithDeps(context.Background(), RetrainOptions{
		OutputPath: out,
		Seed:       1,
		Epochs:     1,
	}, deps)
	if err == nil {
		t.Fatalf("expected close error; got nil")
	}
	if !strings.Contains(err.Error(), "close") {
		t.Errorf("expected 'close' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "simulated close") {
		t.Errorf("expected wrapped underlying close cause, got: %v", err)
	}
	if len(removed) != 1 || removed[0] != tmpSeen {
		t.Errorf("expected remove(%q), got %v", tmpSeen, removed)
	}
}

func TestRetrainAndPersist_RenameFailure(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "rename-fail.bin")
	var removed []string
	wantErr := errors.New("simulated rename EPERM")

	deps := retrainPersistDeps{
		openSink: openFileSink,
		rename:   func(_, _ string) error { return wantErr },
		remove:   recordingRemove(&removed),
	}
	err := retrainAndPersistWithDeps(context.Background(), RetrainOptions{
		OutputPath: out,
		Seed:       1,
		Epochs:     1,
	}, deps)
	if err == nil {
		t.Fatalf("expected rename error; got nil")
	}
	if !strings.Contains(err.Error(), "rename") {
		t.Errorf("expected 'rename' in error, got: %v", err)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped rename cause, got: %v", err)
	}
	if len(removed) != 1 || removed[0] != out+".tmp" {
		t.Errorf("expected remove(%q), got %v", out+".tmp", removed)
	}

	if _, statErr := os.Stat(out + ".tmp"); !os.IsNotExist(statErr) {
		t.Errorf("expected .tmp removed on rename failure, got stat=%v", statErr)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("expected canonical path absent on rename failure, got stat=%v", statErr)
	}
}

func TestRetrainAndPersist_OpenSinkFailure(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "open-fail.bin")
	var removed []string
	wantErr := errors.New("simulated open ENOSPC")

	deps := retrainPersistDeps{
		openSink: func(string) (fileSink, string, error) { return nil, "", wantErr },
		rename:   os.Rename,
		remove:   recordingRemove(&removed),
	}
	err := retrainAndPersistWithDeps(context.Background(), RetrainOptions{
		OutputPath: out,
		Seed:       1,
		Epochs:     1,
	}, deps)
	if err == nil {
		t.Fatalf("expected open error; got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped open cause, got: %v", err)
	}

	if len(removed) != 0 {
		t.Errorf("expected no removes on factory-open failure, got %v", removed)
	}
}

func TestRetrainAndPersist_PropagatesFitErrorOnCtxCancel(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "fit-ctx-cancel.bin")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := retrainAndPersistWithDeps(ctx, RetrainOptions{
		OutputPath: out,
		Seed:       1,
		Epochs:     5,
	}, defaultRetrainPersistDeps())
	if err == nil {
		t.Fatalf("expected fit error on cancelled ctx; got nil")
	}
	if !strings.Contains(err.Error(), "fit") {
		t.Errorf("expected 'fit' in error, got: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected wrapped context.Canceled, got: %v", err)
	}

	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("expected canonical path absent on fit failure, got stat=%v", statErr)
	}
	if _, statErr := os.Stat(out + ".tmp"); !os.IsNotExist(statErr) {
		t.Errorf("expected .tmp absent on fit failure (no open called), got stat=%v", statErr)
	}
}

func TestRetrainAndPersistWithDeps_DefaultDepsMatchProduction(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "default-deps.bin")
	if err := retrainAndPersistWithDeps(context.Background(), RetrainOptions{
		OutputPath: out,
		Seed:       7,
		Epochs:     1,
	}, defaultRetrainPersistDeps()); err != nil {
		t.Fatalf("retrainAndPersistWithDeps(default): %v", err)
	}
	cls, err := LoadLogisticClassifier(out)
	if err != nil {
		t.Fatalf("LoadLogisticClassifier: %v", err)
	}
	if cls.CheckpointHash() == "" {
		t.Errorf("loaded classifier has empty CheckpointHash")
	}
}

func TestRetrainAndPersist_DefaultEpochs(t *testing.T) {

	if testing.Short() {
		t.Skip("default-epochs coverage uses full 30-epoch training; skipped under -short")
	}
	tmp := t.TempDir()
	out := filepath.Join(tmp, "default-epochs.bin")

	if err := RetrainAndPersist(context.Background(), RetrainOptions{
		OutputPath: out,
		Seed:       11,
	}); err != nil {
		t.Fatalf("RetrainAndPersist with default epochs: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected checkpoint at %s: %v", out, err)
	}
}

type failingWriteSink struct {
	closed bool
}

func (s *failingWriteSink) Write(_ []byte) (int, error) {
	return 0, errors.New("simulated write failure")
}
func (s *failingWriteSink) Sync() error  { return nil }
func (s *failingWriteSink) Close() error { s.closed = true; return nil }

func TestRetrainAndPersist_WriteCheckpointFailure(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "write-fail.bin")
	var removed []string
	var tmpSeen string
	factory := func(path string) (fileSink, string, error) {
		tmpSeen = path + ".tmp"
		return &failingWriteSink{}, tmpSeen, nil
	}
	deps := retrainPersistDeps{
		openSink: factory,
		rename:   os.Rename,
		remove:   recordingRemove(&removed),
	}
	err := retrainAndPersistWithDeps(context.Background(), RetrainOptions{
		OutputPath: out,
		Seed:       1,
		Epochs:     1,
	}, deps)
	if err == nil {
		t.Fatalf("expected write-checkpoint error; got nil")
	}
	if !strings.Contains(err.Error(), "write checkpoint") {
		t.Errorf("expected 'write checkpoint' in error, got: %v", err)
	}
	if len(removed) != 1 || removed[0] != tmpSeen {
		t.Errorf("expected remove(%q), got %v", tmpSeen, removed)
	}
}

// =============================================================================
// IMPORTANT-2: ScoreSoftmax concurrent safety
//
// Documented contract on QueryClassifier (router.go): "Implementations MUST
// be safe for concurrent ScoreSoftmax calls". This test exercises that
// guarantee for *LogisticClassifier under -race + asserts bit-stable output
// across goroutines for the same input.
// =============================================================================

// TestLogisticClassifier_ScoreSoftmaxParallel runs GOMAXPROCS workers ×
// `iter` iterations against a single trained classifier with a shared
// probe embedding. Asserts:
// - no -race violation (the test MUST be run with `-race`)
// - every result is bitwise identical to the single-threaded baseline
// (within 1e-9 to allow for floating-point summation reorder, which
// should NOT happen here because the algorithm is deterministic on
// fixed inputs, but the tolerance buys us against any future inner-loop
// refactor that introduced FMA-induced jitter).
func TestLogisticClassifier_ScoreSoftmaxParallel(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	corpus := generateLabeledCorpus(rng, 500)
	cls := NewLogisticClassifier(LogisticClassifierConfig{
		EmbeddingDim: 1536,
		Ecosystems:   AllEcosystems,
		Seed:         42,
		Epochs:       10,
	})
	if err := cls.Fit(context.Background(), corpus); err != nil {
		t.Fatalf("Fit: %v", err)
	}
	probe := syntheticEmbedding(rng, EcoGo)
	expected, err := cls.ScoreSoftmax(context.Background(), probe)
	if err != nil {
		t.Fatalf("baseline ScoreSoftmax: %v", err)
	}

	workers := runtime.GOMAXPROCS(0)
	if workers < 2 {
		workers = 2
	}
	const iter = 100
	var wg sync.WaitGroup
	errs := make(chan error, workers*iter)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iter; i++ {
				got, gerr := cls.ScoreSoftmax(context.Background(), probe)
				if gerr != nil {
					errs <- gerr
					return
				}
				for k, v := range expected {
					if math.Abs(got[k]-v) > 1e-9 {
						errs <- fmt.Errorf("parallel ScoreSoftmax mismatch on %s: got %g want %g", k, got[k], v)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("%v", e)
	}
}

func BenchmarkLogisticClassifier_ScoreSoftmax(b *testing.B) {
	rng := rand.New(rand.NewSource(42))
	corpus := generateLabeledCorpus(rng, 1000)
	cls := NewLogisticClassifier(LogisticClassifierConfig{
		EmbeddingDim: 1536,
		Ecosystems:   AllEcosystems,
		Seed:         42,
		Epochs:       10,
	})
	if err := cls.Fit(context.Background(), corpus); err != nil {
		b.Fatalf("Fit: %v", err)
	}
	probe := syntheticEmbedding(rng, EcoGo)
	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := cls.ScoreSoftmax(ctx, probe); err != nil {
			b.Fatalf("ScoreSoftmax[%d]: %v", i, err)
		}
	}
}

func TestLogisticClassifier_ScoreSoftmaxLatencyBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("perf assertion; skipped under -short")
	}
	rng := rand.New(rand.NewSource(42))
	corpus := generateLabeledCorpus(rng, 500)
	cls := NewLogisticClassifier(LogisticClassifierConfig{
		EmbeddingDim: 1536,
		Ecosystems:   AllEcosystems,
		Seed:         42,
		Epochs:       5,
	})
	if err := cls.Fit(context.Background(), corpus); err != nil {
		t.Fatalf("Fit: %v", err)
	}
	probe := syntheticEmbedding(rng, EcoGo)
	ctx := context.Background()

	for i := 0; i < 50; i++ {
		if _, err := cls.ScoreSoftmax(ctx, probe); err != nil {
			t.Fatalf("warmup[%d]: %v", i, err)
		}
	}

	const samples = 1000
	const budget = 2 * time.Millisecond
	start := time.Now()
	for i := 0; i < samples; i++ {
		if _, err := cls.ScoreSoftmax(ctx, probe); err != nil {
			t.Fatalf("ScoreSoftmax[%d]: %v", i, err)
		}
	}
	mean := time.Since(start) / samples
	if mean > budget {
		t.Errorf("ScoreSoftmax mean latency %v exceeds %v budget (was the hot path regressed?)", mean, budget)
	}
}
