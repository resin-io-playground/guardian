package rundmc

import (
	"encoding/json"
	"io/ioutil"
	"os/exec"
	"path/filepath"

	"github.com/opencontainers/specs"
)

type Bundle struct {
	configJSON  []byte
	runtimeJSON []byte
}

type namespaceType string

type namespace struct {
	NsType namespaceType `json:"type"`
	Path   string        `json:"path"`
}

func (b Bundle) Create(path string) error {
	err := ioutil.WriteFile(filepath.Join(path, "config.json"), b.configJSON, 0700)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(filepath.Join(path, "runtime.json"), b.runtimeJSON, 0700)
	if err != nil {
		return err
	}
	return nil
}

func BundleForCmd(cmd *exec.Cmd) *Bundle {
	configJson, err := json.Marshal(specs.Spec{
		Version: "0.1.0",
		Process: specs.Process{
			Terminal: true,
			Args:     cmd.Args,
		},
	})

	if err != nil {
		panic(err) // can't happen
	}

	runtimeJson, err := json.Marshal(RuntimeSpec{
		Linux: Linux{
			Namespaces: namespaces(),
		},
	})
	if err != nil {
		panic(err) // can't happen
	}

	return &Bundle{
		configJSON:  configJson,
		runtimeJSON: runtimeJson,
	}
}

func namespaces() []namespace {
	namespaces := make([]namespace, 6)
	nsTypes := []namespaceType{"pid", "network", "mount", "ipc", "uts", "user"}
	for i, nsType := range nsTypes {
		namespaces[i] = namespace{
			NsType: nsType,
			Path:   ""}
	}
	return namespaces
}

type RuntimeSpec struct {
	Linux Linux `json:"linux"`
}

type Linux struct {
	Namespaces []namespace `json:"namespaces"`
}
