package contract

import "encoding/json"

// MarshalJSON writes the projects of a flow.applied event even when there
// are none: the schema has them optional only for the events earlier
// versions of Gentry wrote, and every event of this version has them.
func (d FlowAppliedData) MarshalJSON() ([]byte, error) {
	projects := d.Projects
	if projects == nil {
		projects = []string{}
	}
	return json.Marshal(struct {
		Commit   string   `json:"commit"`
		Projects []string `json:"projects"`
	}{d.Commit, projects})
}
