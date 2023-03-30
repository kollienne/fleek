package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ublue-os/fleek/internal/debug"
	"gopkg.in/yaml.v3"
)

var (
	operatingSystems = []string{"linux", "darwin"}
	architectures    = []string{"aarch64", "x86_64"}
	shells           = []string{"bash", "zsh"}
	blingLevels      = []string{"low", "default", "high"}
	LowPackages      = []string{"htop", "git", "github-cli", "glab"}
	DefaultPackages  = []string{"fzf", "ripgrep", "vscode"}
	HighPackages     = []string{"lazygit", "jq", "yq", "neovim", "neofetch", "btop", "cheat"}
	LowPrograms      = []string{"starship"}
	DefaultPrograms  = []string{"direnv"}
	HighPrograms     = []string{"exa", "bat", "atuin", "zoxide"}
)

// Config holds the options that will be
// merged into the home-manager flake.
type Config struct {
	FlakeDir string `yaml:"flakedir"`
	Unfree   bool   `yaml:"unfree"`
	// bash or zsh
	Shell string `yaml:"shell"`
	// low, default, high
	Bling      string            `yaml:"bling"`
	Repository string            `yaml:"repo"`
	Name       string            `yaml:"name"`
	Packages   []string          `yaml:",flow"`
	Programs   []string          `yaml:",flow"`
	Aliases    map[string]string `yaml:",flow"`
	Paths      []string          `yaml:"paths"`
	Ejected    bool              `yaml:"ejected"`
	Systems    []System          `yaml:",flow"`
}
type GitConfig struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

type System struct {
	Hostname  string    `yaml:"hostname"`
	Username  string    `yaml:"username"`
	Arch      string    `yaml:"arch"`
	OS        string    `yaml:"os"`
	GitConfig GitConfig `yaml:"git"`
}

func (s System) HomeDir() string {
	base := "/home"
	if s.OS == "darwin" {
		base = "/Users"
	}
	return base + "/" + s.Username
}

func NewSystem(name, email string) (*System, error) {
	user, err := Username()
	if err != nil {
		return nil, err
	}
	host, err := Hostname()
	if err != nil {
		return nil, err
	}
	return &System{
		Hostname: host,
		Arch:     Arch(),
		OS:       runtime.GOOS,
		Username: user,
		GitConfig: GitConfig{
			Name:  name,
			Email: email,
		},
	}, nil
}

var (
	ErrMissingFlakeDir        = errors.New("fleek.yml: missing `flakedir`")
	ErrInvalidShell           = errors.New("fleek.yml: invalid shell, valid shells are: " + strings.Join(shells, ", "))
	ErrInvalidBling           = errors.New("fleek.yml: invalid bling level, valid levels are: " + strings.Join(blingLevels, ", "))
	ErrorInvalidArch          = errors.New("fleek.yml: invalid architecture, valid architectures are: " + strings.Join(architectures, ", "))
	ErrInvalidOperatingSystem = errors.New("fleek.yml: invalid OS, valid operating systems are: " + strings.Join(operatingSystems, ", "))
	ErrPackageNotFound        = errors.New("package not found in configuration file")
	ErrProgramNotFound        = errors.New("program not found in configuration file")
)

func (c *Config) Validate() error {
	if c.FlakeDir == "" {
		return ErrMissingFlakeDir
	}
	if !isValueInList(c.Shell, shells) {
		return ErrInvalidShell
	}
	if !isValueInList(c.Bling, blingLevels) {
		return ErrInvalidBling
	}
	for _, sys := range c.Systems {
		if !isValueInList(sys.Arch, architectures) {
			return ErrorInvalidArch
		}

		if !isValueInList(sys.OS, operatingSystems) {
			return ErrInvalidOperatingSystem
		}
	}
	return nil
}

func isValueInList(value string, list []string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

func (c *Config) UserFlakeDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, c.FlakeDir)
}

func (c *Config) AddPackage(pack string) error {
	var found bool
	for _, p := range c.Packages {
		if p == pack {
			found = true
			break
		}
	}
	if found {
		return nil
	}
	c.Packages = append(c.Packages, pack)
	err := c.Validate()
	if err != nil {
		return err
	}
	return c.Save()
}
func (c *Config) RemovePackage(pack string) error {
	var index int
	var found bool
	for x, p := range c.Packages {
		if p == pack {
			index = x
			found = true
			break
		}
	}
	if found {
		c.Packages = append(c.Packages[:index], c.Packages[index+1:]...)
	} else {
		return ErrPackageNotFound
	}
	err := c.Validate()
	if err != nil {
		return err
	}
	return c.Save()
}
func (c *Config) RemoveProgram(prog string) error {
	var index int
	var found bool
	for x, p := range c.Programs {
		if p == prog {
			index = x
			found = true
			break
		}
	}
	if found {
		c.Programs = append(c.Programs[:index], c.Programs[index+1:]...)
	} else {
		return ErrProgramNotFound
	}
	err := c.Validate()
	if err != nil {
		return err
	}
	return c.Save()
}
func (c *Config) AddProgram(prog string) error {
	c.Programs = append(c.Programs, prog)
	err := c.Validate()
	if err != nil {
		return err
	}
	return c.Save()
}

func (c *Config) Save() error {
	cfile, err := c.Location()
	if err != nil {
		return err
	}
	cfg, err := os.Create(cfile)
	if err != nil {
		return err
	}
	bb, err := yaml.Marshal(&c)
	if err != nil {
		return err
	}
	m := make(map[interface{}]interface{})
	err = yaml.Unmarshal(bb, &m)
	if err != nil {
		return err
	}
	n, err := yaml.Marshal(&m)
	if err != nil {
		return err
	}
	// convert to string to get `-` style lists
	sbb := string(n)
	_, err = cfg.WriteString(sbb)
	if err != nil {
		return err
	}
	return nil
}

// ReadConfig returns the configuration data
// stored in $HOME/.fleek.yml
func ReadConfig() (*Config, error) {
	c := &Config{}
	home, err := os.UserHomeDir()
	if err != nil {
		return c, err
	}
	csym := filepath.Join(home, ".fleek.yml")
	bb, err := os.ReadFile(csym)
	if err != nil {
		return c, err
	}
	err = yaml.Unmarshal(bb, c)
	if err != nil {
		return c, err
	}
	return c, nil
}

func (c *Config) Clone(repo string) error {

	clone := exec.Command("git", "clone", "-q", repo, c.UserFlakeDir())
	clone.Stderr = os.Stderr
	clone.Stdin = os.Stdin
	clone.Stdout = os.Stdout
	clone.Env = os.Environ()

	err := clone.Run()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	yamlPath := filepath.Join(c.UserFlakeDir(), ".fleek.yml")
	csym := filepath.Join(home, ".fleek.yml")
	return os.Symlink(yamlPath, csym)

}

// WriteSampleConfig creates the first fleek
// configuration file
func WriteSampleConfig(location, email, name string, force bool) error {
	aliases := make(map[string]string)
	aliases["cdfleek"] = "cd ~/.config/home-manager"
	sys, err := NewSystem(name, email)
	if err != nil {
		debug.Log("new system err: %s ", err)
		return err
	}
	c := &Config{
		FlakeDir: location,
		Unfree:   true,
		Shell:    "bash",
		Bling:    "default",
		Name:     "My Fleek Configuration",
		Packages: []string{
			"helix",
		},
		Programs: []string{
			"dircolors",
		},
		Aliases: aliases,
		Paths: []string{
			"$HOME/bin",
			"$HOME/.local/bin",
		},
		Systems: []System{*sys},
	}
	cfile, err := c.Location()
	if err != nil {
		debug.Log("location err: %s ", err)
		return err
	}
	debug.Log("cfile: %s ", cfile)

	err = c.MakeFlakeDir()
	if err != nil {
		return fmt.Errorf("making flake dir: %s", err)
	}
	_, err = os.Stat(cfile)

	debug.Log("stat err: %v ", err)
	debug.Log("force: %v ", force)

	if force || errors.Is(err, fs.ErrNotExist) {

		cfg, err := os.Create(cfile)
		if err != nil {
			return err
		}
		bb, err := yaml.Marshal(&c)
		if err != nil {
			return err
		}
		m := make(map[interface{}]interface{})
		err = yaml.Unmarshal(bb, &m)
		if err != nil {
			return err
		}
		n, err := yaml.Marshal(&m)
		if err != nil {
			return err
		}
		// convert to string to get `-` style lists
		sbb := string(n)
		_, err = cfg.WriteString(sbb)
		if err != nil {
			return err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		csym := filepath.Join(home, ".fleek.yml")
		err = os.Symlink(cfile, csym)
		if err != nil {
			return err
		}
	} else {
		return errors.New("cowardly refusing to overwrite config file without --force flag")
	}
	return nil
}

// WriteEjectConfig updates the .fleek.yml file
// to indicated ejected status
func (c *Config) Eject() error {

	c.Ejected = true

	cfile, err := c.Location()
	if err != nil {
		return err
	}

	bb, err := yaml.Marshal(&c)
	if err != nil {
		return err
	}
	m := make(map[interface{}]interface{})
	err = yaml.Unmarshal(bb, &m)
	if err != nil {
		return err
	}
	n, err := yaml.Marshal(&m)
	if err != nil {
		return err
	}

	err = os.WriteFile(cfile, n, 0755)
	if err != nil {
		return err
	}

	return nil
}