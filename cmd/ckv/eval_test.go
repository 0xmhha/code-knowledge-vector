package main

import (
	"math"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/internal/eval/prregress"
)

func TestMeanStd_Empty(t *testing.T) {
	mean, std := meanStd(nil)
	if mean != 0 || std != 0 {
		t.Errorf("empty: got mean=%v std=%v, want 0/0", mean, std)
	}
}

func TestMeanStd_Single(t *testing.T) {
	mean, std := meanStd([]float64{0.5})
	if mean != 0.5 {
		t.Errorf("single mean=%v, want 0.5", mean)
	}
	if std != 0 {
		t.Errorf("single std=%v, want 0 (sample std with N=1 is undefined → 0)", std)
	}
}

func TestMeanStd_KnownValues(t *testing.T) {
	// xs = [0.2, 0.4, 0.6, 0.8] → mean 0.5, sample variance = 0.6667e-1
	// sample std ≈ 0.2582 (denominator N-1=3)
	xs := []float64{0.2, 0.4, 0.6, 0.8}
	mean, std := meanStd(xs)
	if math.Abs(mean-0.5) > 1e-9 {
		t.Errorf("mean=%v, want 0.5", mean)
	}
	want := 0.2581988897471611
	if math.Abs(std-want) > 1e-6 {
		t.Errorf("std=%v, want ~%v", std, want)
	}
}

func TestAggregateRuns_SinglePass(t *testing.T) {
	entry := prregress.Entry{ID: "p1", Threshold: 0.8}
	runs := []prregress.Result{
		{Entry: entry, Pass: true, Score: prregress.Score{
			JudgeScore: 0.9, FileF1: 1.0, FilePrecision: 1.0, FileRecall: 1.0,
		}},
	}
	s := aggregateRuns(entry, runs)
	if s.JudgeMean != 0.9 {
		t.Errorf("JudgeMean=%v, want 0.9", s.JudgeMean)
	}
	if s.JudgeStd != 0 {
		t.Errorf("JudgeStd=%v, want 0 for N=1", s.JudgeStd)
	}
	if s.PassRate != 1.0 {
		t.Errorf("PassRate=%v, want 1.0", s.PassRate)
	}
	if s.AnyError {
		t.Error("AnyError should be false on clean run")
	}
}

func TestAggregateRuns_MultiMixedPass(t *testing.T) {
	entry := prregress.Entry{ID: "p1", Threshold: 0.8}
	runs := []prregress.Result{
		{Entry: entry, Pass: true, Score: prregress.Score{JudgeScore: 1.0, FileF1: 1.0}},
		{Entry: entry, Pass: true, Score: prregress.Score{JudgeScore: 0.8, FileF1: 0.8}},
		{Entry: entry, Pass: false, Score: prregress.Score{JudgeScore: 0.6, FileF1: 0.6}},
		{Entry: entry, Pass: true, Score: prregress.Score{JudgeScore: 0.9, FileF1: 0.9}},
		{Entry: entry, Pass: false, Score: prregress.Score{JudgeScore: 0.5, FileF1: 0.5}},
	}
	s := aggregateRuns(entry, runs)
	// mean of [1.0, 0.8, 0.6, 0.9, 0.5] = 0.76
	if math.Abs(s.JudgeMean-0.76) > 1e-9 {
		t.Errorf("JudgeMean=%v, want 0.76", s.JudgeMean)
	}
	// pass count = 3 out of 5
	if math.Abs(s.PassRate-0.6) > 1e-9 {
		t.Errorf("PassRate=%v, want 0.6", s.PassRate)
	}
	if s.JudgeStd == 0 {
		t.Error("JudgeStd should be >0 for varied inputs")
	}
}

func TestAggregateRuns_WithErrors(t *testing.T) {
	entry := prregress.Entry{ID: "p1", Threshold: 0.8}
	runs := []prregress.Result{
		{Entry: entry, Error: "agent timeout"},
		{Entry: entry, Pass: true, Score: prregress.Score{JudgeScore: 0.9}},
		{Entry: entry, Error: "judge crashed"},
	}
	s := aggregateRuns(entry, runs)
	if s.ErrorCount != 2 {
		t.Errorf("ErrorCount=%d, want 2", s.ErrorCount)
	}
	if !s.AnyError {
		t.Error("AnyError should be true")
	}
	// PassRate denominator includes erroneous runs: 1 pass / 3 total = 0.333
	if math.Abs(s.PassRate-1.0/3.0) > 1e-9 {
		t.Errorf("PassRate=%v, want 1/3", s.PassRate)
	}
	// Mean computed only over successful runs (one): 0.9
	if s.JudgeMean != 0.9 {
		t.Errorf("JudgeMean=%v, want 0.9 (errors excluded from mean)", s.JudgeMean)
	}
}
