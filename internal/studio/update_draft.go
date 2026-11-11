package studio

import (
	"image2ai/internal/domain"
	"image2ai/internal/generation"

	"github.com/egoist/mygo/ui"
)

func (a *App) updateDraft() domain.UpdateDraft {
	draft := domain.UpdateDraft{
		Prompt: a.prompt, References: append([]domain.Reference(nil), a.references...),
		Size: a.draftSize, Markup: a.draftMarkup,
		Preferences: domain.GenerationPreferences{ProviderID: a.providerID, Model: a.model,
			Ratio: a.ratio, Resolution: a.resolution, OutputFormat: a.outputFormat,
			GIFSize: a.gifSize, GIFFrameCount: a.gifFrameCount, GIFFrameDurationMS: a.gifFrameDurationMS},
	}
	for _, task := range a.jobs {
		if task.Progress.Status == "error" {
			draft.FailedJobs = append(draft.FailedJobs, domain.UpdateFailedJob{Args: task.Args, Progress: task.Progress, ProviderName: task.ProviderName})
		}
	}
	return draft
}

func updateDraftThumbnails(draft *domain.UpdateDraft) (map[string]*ui.Bitmap, error) {
	thumbs := make(map[string]*ui.Bitmap, len(draft.References))
	for _, ref := range draft.References {
		data, _, err := generation.ValidateReference(ref.DataURL)
		if err != nil {
			return nil, err
		}
		thumb, err := thumbnail(data, referenceThumbSize, referenceThumbSize)
		if err != nil {
			return nil, err
		}
		thumbs[ref.ID] = thumb
	}
	return thumbs, nil
}

func (a *App) restoreUpdateDraft(draft *domain.UpdateDraft, thumbs map[string]*ui.Bitmap) {
	a.prompt, a.references, a.referenceThumbs = draft.Prompt, draft.References, thumbs
	a.draftSize, a.draftMarkup = draft.Size, draft.Markup
	p := draft.Preferences
	a.providerID, a.model, a.ratio, a.resolution = p.ProviderID, p.Model, p.Ratio, p.Resolution
	a.outputFormat = p.OutputFormat
	a.gifSize, a.gifFrameCount, a.gifFrameDurationMS = p.GIFSize, p.GIFFrameCount, p.GIFFrameDurationMS
	for _, failed := range draft.FailedJobs {
		a.jobs = append(a.jobs, &job{Args: failed.Args, Progress: failed.Progress, ProviderName: failed.ProviderName})
	}
}
