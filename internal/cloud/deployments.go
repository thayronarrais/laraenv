package cloud

import (
	"context"
)

// Deployment mirrors the server's Deployment row + its serialized response.
type Deployment struct {
	ID                int                    `json:"id"`
	Name              string                 `json:"name"`
	SSHHostExternalID string                 `json:"ssh_host_external_id"`
	OSFamily          string                 `json:"os_family,omitempty"`
	OSVersion         string                 `json:"os_version,omitempty"`
	ProjectKind       string                 `json:"project_kind,omitempty"`
	Config            map[string]any         `json:"config,omitempty"`
	LastStatus        string                 `json:"last_status,omitempty"`
	LastRunAt         string                 `json:"last_run_at,omitempty"`
	Runs              []DeploymentRunSummary `json:"runs,omitempty"`
}

// DeploymentRunSummary is a slim view of one entry in deployment.runs.
type DeploymentRunSummary struct {
	ID           int    `json:"id"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at,omitempty"`
	HasLog       bool   `json:"has_log"`
}

type listDeploymentsResponse struct {
	Deployments []Deployment `json:"deployments"`
}

func (c *Client) ListDeployments(ctx context.Context) ([]Deployment, error) {
	if c.token() == "" {
		return nil, ErrUnauthenticated
	}
	var out listDeploymentsResponse
	if err := c.get(ctx, "/api/v1/deployments", &out); err != nil {
		return nil, err
	}
	return out.Deployments, nil
}

func (c *Client) GetDeployment(ctx context.Context, id int) (*Deployment, error) {
	if c.token() == "" {
		return nil, ErrUnauthenticated
	}
	var d Deployment
	path := "/api/v1/deployments/" + intStr(id)
	if err := c.get(ctx, path, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (c *Client) CreateDeployment(ctx context.Context, name, sshHostExternalID string) (*Deployment, error) {
	if c.token() == "" {
		return nil, ErrUnauthenticated
	}
	body := map[string]string{
		"name":                 name,
		"ssh_host_external_id": sshHostExternalID,
	}
	var d Deployment
	if err := c.post(ctx, "/api/v1/deployments", body, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (c *Client) DeleteDeployment(ctx context.Context, id int) error {
	if c.token() == "" {
		return ErrUnauthenticated
	}
	return c.delete(ctx, "/api/v1/deployments/"+intStr(id))
}

// PatchDeployment sends a partial update (PATCH) to the server. Used by
// the detection step to persist os_family/os_version/project_kind/config
// without overwriting unrelated fields.
func (c *Client) PatchDeployment(ctx context.Context, id int, patch map[string]any) (*Deployment, error) {
	if c.token() == "" {
		return nil, ErrUnauthenticated
	}
	var d Deployment
	if err := c.patch(ctx, "/api/v1/deployments/"+intStr(id), patch, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// StoreDeploymentRun creates an audit-trail row for a finished detection /
// apply pass. `logCiphertext` is the b64-encoded encrypted log (or "" to
// skip log persistence).
func (c *Client) StoreDeploymentRun(ctx context.Context, id int, kind, status, logCiphertextB64, errMsg, startedAt, finishedAt string) error {
	if c.token() == "" {
		return ErrUnauthenticated
	}
	body := map[string]any{
		"kind":           kind,
		"status":         status,
		"started_at":     startedAt,
		"finished_at":    finishedAt,
		"error_message":  errMsg,
		"log_ciphertext": logCiphertextB64,
	}
	return c.post(ctx, "/api/v1/deployments/"+intStr(id)+"/runs", body, nil)
}

// intStr converts an int to its decimal string. Avoids importing strconv
// just for this one helper since the rest of the file is already lean.
func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
