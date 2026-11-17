package api

import (
	"context"
	"strings"

	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// settingTaskTypes is the setting naming the task types the AI may propose (AI-32).
const settingTaskTypes = "ai.task_types"

// catalogue is the AI's task catalogue (§9.6): Deps.Catalogue, else the release's.
// proposable are the task types a turn may propose now (AI-32; design D7): those on for the organisation that the
// release's catalogue has, with their command built and nothing on the never-list.
func (a *API) proposable(ctx context.Context, u storage.UnitOfWork) ([]string, error) {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return nil, err
	}
	on, err := settingOn(ctx, u, settingTaskTypes, today)
	if err != nil || on == "" {
		return nil, err
	}
	var out []string
	for _, id := range strings.Split(on, ",") {
		tt, ok := a.catalogue().Get(id)
		if _, built := a.commands()[tt.Command]; ok && built && proposal.Refuse(tt.ID, tt.Command) == "" {
			out = append(out, id)
		}
	}
	return out, nil
}

func (a *API) catalogue() *proposal.Catalogue {
	if a.deps.Catalogue != nil {
		return a.deps.Catalogue
	}
	return proposal.Default()
}
