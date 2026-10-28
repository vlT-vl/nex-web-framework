package updater

type HealthMode string

const (
	HealthFrontend HealthMode = "frontend"
	HealthBackend  HealthMode = "backend"
)

type InstallMode string

const (
	InstallSelf   InstallMode = "self"
	InstallNotify InstallMode = "notify"
)

type Host interface {
	AppID() string
	Version() (version, build string)
	StateDir() string
	Authorize(op string) error
	Emit(event string, payload any)
	Quiesce() error
	RelaunchEnv() []string
	HealthMode() HealthMode
	InstallMode() InstallMode
}
