package api

import "github.com/cognizhi/fyniq/internal/proposal"

// settingTaskTypes is the setting naming the task types the AI may propose (AI-32).
const settingTaskTypes = "ai.task_types"

// catalogue is the AI's task catalogue (§9.6): Deps.Catalogue, else the release's.
func (a *API) catalogue() *proposal.Catalogue {
	if a.deps.Catalogue != nil {
		return a.deps.Catalogue
	}
	return proposal.Default()
}
