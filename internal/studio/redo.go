package studio

import "errors"
import "image2ai/internal/locale"
import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
	"image2ai/internal/generation"
)

func (a *App) redoJob(task *job) {
	args := task.Args
	args.ReferenceImages = append([]string(nil), args.ReferenceImages...)
	a.restoreGenerationDraft(args, func() ([]domain.Reference, error) {
		refs := make([]domain.Reference, 0, len(args.ReferenceImages))
		for i, value := range args.ReferenceImages {
			refs = append(refs, domain.Reference{ID: newID(), Name: fmt.Sprintf(locale.T("参考图 %d"), i+1), DataURL: value})
		}
		return refs, nil
	}, func() { a.removeJob(task) })
}

// Redo and artwork reuse share one atomic draft replacement. Read/decode first,
// so an unavailable reference cannot destroy the current draft or failed task.
func (a *App) restoreGenerationDraft(args domain.GenerateArgs, read func() ([]domain.Reference, error), restored func()) {
	if a.updateRestarting() || a.importing {
		return
	}
	a.referenceEpoch++
	epoch := a.referenceEpoch
	a.importing = true
	a.run(func() func() {
		refs, err := read()
		thumbs := make(map[string]*ui.Bitmap)
		if err == nil && len(refs) > domain.MaxReferenceImages {
			err = fmt.Errorf(locale.T("最多恢复 %d 张参考图。"), domain.MaxReferenceImages)
		}
		for _, ref := range refs {
			if err != nil {
				break
			}
			var data []byte
			data, _, err = generation.ValidateReference(ref.DataURL)
			if err == nil {
				thumbs[ref.ID], err = thumbnail(data, referenceThumbSize, referenceThumbSize)
			}
		}
		if err == nil && args.Markup != "" {
			if len(refs) != 1 {
				err = errors.New(locale.T("标注编辑需要一张原图。"))
			} else {
				_, err = decodeMarkupBase(args.Markup, refs[0].DataURL)
			}
		}
		return func() {
			if epoch != a.referenceEpoch {
				return
			}
			a.importing = false
			if err != nil {
				a.problem = err.Error()
				return
			}
			a.prompt, _ = compactPrompt(args.Prompt, [2]int{})
			a.providerID, a.model, a.outputFormat = args.ProviderID, args.Model, args.OutputFormat
			a.ratio, a.resolution = generation.FindSizeCombo(args.Size)
			a.draftSize, a.draftMarkup = "", args.Markup
			if args.OutputFormat == "gif" {
				a.reuseGIFSettings(domain.ImageRecord{Size: args.Size, GIFFrameCount: args.FrameCount, GIFFrameDurationMS: args.FrameDurationMS})
			} else if args.Size != "" && computeSize(a.ratio, a.resolution) != args.Size {
				a.draftSize = args.Size
			}
			a.references, a.referenceThumbs = refs, thumbs
			a.reconcileSelection()
			a.savePreferences()
			a.loadPricing()
			a.problem = ""
			if restored != nil {
				restored()
			}
			a.showNotice(locale.T("已带入提示词、参考图和生成参数，修改后点击开始生成。"))
		}
	})
}
