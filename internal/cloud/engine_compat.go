package cloud

// Keep the hosted API's engine contracts available to existing callers while
// the account-independent implementation lives in internal/deploy.
import (
	"lara4ever/internal/config"
	"lara4ever/internal/deploy"

	xssh "golang.org/x/crypto/ssh"
)

type ApplyConfig = deploy.ApplyConfig
type DetectionResult = deploy.DetectionResult
type Step = deploy.Step
type RenderedScript = deploy.RenderedScript

func NormalizeGitURL(rawURL string) string {
	return deploy.NormalizeGitURL(rawURL)
}

func SpliceGitPAT(rawURL, pat string) string {
	return deploy.SpliceGitPAT(rawURL, pat)
}

func BuildPlan(cfg ApplyConfig, det *DetectionResult, deployScript string, legacy bool) []Step {
	return deploy.BuildPlan(cfg, det, deployScript, legacy)
}

func BuildDestroyPlan(cfg ApplyConfig) []Step {
	return deploy.BuildDestroyPlan(cfg)
}

func RunApply(client *xssh.Client, steps []Step, onLine func(string), onStep func(idx, total int, title, status string)) (string, error) {
	return deploy.RunApply(client, steps, onLine, onStep)
}

func RunDetection(client *xssh.Client, onLine func(string)) (*DetectionResult, error) {
	return deploy.RunDetection(client, onLine)
}

func BuiltinPresets() []config.DeployPreset {
	return deploy.BuiltinPresets()
}

func FindPreset(cfg *config.Store, id string) (*config.DeployPreset, bool) {
	return deploy.FindPreset(cfg, id)
}

func RenderDeployScript(script string, cfg ApplyConfig) (rendered string, unresolved []string, warnings []string) {
	return deploy.RenderDeployScript(script, cfg)
}

func ResolveDeployScript(cfg ApplyConfig, lookup func(id string) (string, bool)) (script string, legacy bool, err error) {
	return deploy.ResolveDeployScript(cfg, lookup)
}

func BuildDeployScript(cfg ApplyConfig, pipeline string) string {
	return deploy.BuildDeployScript(cfg, pipeline)
}

func DeployNowScript(cfg ApplyConfig, lookup func(id string) (string, bool)) (RenderedScript, error) {
	return deploy.DeployNowScript(cfg, lookup)
}

func RenderDeployPipeline(cfg ApplyConfig, lookup func(id string) (string, bool)) (RenderedScript, error) {
	return deploy.RenderDeployPipeline(cfg, lookup)
}
