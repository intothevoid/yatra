package services

import (
	"context"

	"github.com/karan/travel-diary/internal/adapters"
)

type modelProgressKey struct{}

type modelProgressReporter func(stage string, characters int)

func withModelProgress(ctx context.Context, report modelProgressReporter) context.Context {
	return context.WithValue(ctx, modelProgressKey{}, report)
}

func reportModelProgress(ctx context.Context, stage string, characters int) {
	if report, ok := ctx.Value(modelProgressKey{}).(modelProgressReporter); ok {
		report(stage, characters)
	}
}

func withModelStage(ctx context.Context, stage string) context.Context {
	return adapters.WithGenerationProgress(ctx, func(characters int) {
		reportModelProgress(ctx, stage, characters)
	})
}
