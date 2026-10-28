package updater

import "time"

const rejectedFile = "updater-rejected.json"

type rejectedRelease struct {
	Version string `json:"version"`
	Build   string `json:"build"`
}

type helperJob struct {
	AppID         string        `json:"appID"`
	Version       string        `json:"version"`
	Build         string        `json:"build"`
	StateDir      string        `json:"stateDir"`
	WorkDir       string        `json:"workDir"`
	StagingRoot   string        `json:"stagingRoot"`
	Staged        string        `json:"staged"`
	Target        string        `json:"target"`
	Backup        string        `json:"backup"`
	LaunchEntry   string        `json:"launchEntry"`
	ParentPID     int           `json:"parentPID"`
	ParentWait    time.Duration `json:"parentWait"`
	HealthTimeout time.Duration `json:"healthTimeout"`
}

type journal struct {
	Phase string    `json:"phase"`
	Job   helperJob `json:"job"`
}
