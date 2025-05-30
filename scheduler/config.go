package scheduler

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/go-viper/mapstructure/v2"

	"github.com/spf13/viper"
)

type Configuration struct {
	Agent AgentConfig `yaml:"agent"`
}

type AgentConfig struct {
	Reporting   bool                `yaml:"reporting"`
	Maintenance []MaintenanceConfig `validate:"dive"`
	Tasks       []Task              `mapstructure:"tasks" validate:"dive"`
}

type Task struct {
	Name       string `validate:"required"`
	Repository string `validate:"required"`

	Backup  *BackupConfig
	Check   []CheckConfig   `validate:"dive"`
	Restore []RestoreConfig `validate:"dive"`
	Sync    []SyncConfig    `validate:"dive"`
}

type BackupConfig struct {
	Name      string
	Tags      []string
	Path      string        `validate:"required"`
	Interval  time.Duration `validate:"required"`
	Check     BackupConfigCheck
	Retention time.Duration
}

// CheckDecodeHook is a mapstructure decode hook to allow users to specify
// "check: <bool>" in the config file to initialize the check with sensible
// defaults, but also with"check: <object>" to allow for more fine-grained
// control.
func BackupConfigCheckDecodeHook() mapstructure.DecodeHookFunc {
	return func(
		from reflect.Type,
		to reflect.Type,
		data interface{},
	) (interface{}, error) {
		// Check if source is bool and target is our CheckField type.
		if from.Kind() == reflect.Bool && to == reflect.TypeOf(BackupConfigCheck{}) {
			enabled, ok := data.(bool)
			if !ok {
				return data, nil
			}
			return BackupConfigCheck{Enabled: enabled}, nil
		}
		return data, nil
	}
}

type BackupConfigCheck struct {
	Enabled bool
}

type CheckConfig struct {
	Path     string `validate:"required"`
	Since    string
	Before   string
	Interval time.Duration `validate:"required"`
	Latest   bool
}

type RestoreConfig struct {
	Path     string        `validate:"required"`
	Target   string        `validate:"required"`
	Interval time.Duration `validate:"required"`
}

type SyncDirection string

const (
	SyncDirectionTo   SyncDirection = "to"
	SyncDirectionFrom SyncDirection = "from"
	SyncDirectionWith SyncDirection = "with"
)

// DirectionDecodeHook is a mapstructure decode hook to force the direction to
// be one of "to", "from", or "with". Note that the hook is not called if the
// optional field "Direction" is not present.
func SyncDirectionDecodeHook() mapstructure.DecodeHookFunc {
	return func(
		from reflect.Type,
		to reflect.Type,
		data interface{},
	) (interface{}, error) {
		if from.Kind() == reflect.String && to == reflect.TypeOf(SyncDirection("")) {
			s := strings.TrimSpace(data.(string))
			var d SyncDirection
			switch s {
			case "to", "from", "with":
				d = SyncDirection(s)
			default:
				return nil, fmt.Errorf("invalid direction %q; must be one of: to, from, with", s)
			}
			return d, nil
		}
		return data, nil
	}
}

func DurationDecodeHook() mapstructure.DecodeHookFunc {
	return func(
		from reflect.Type,
		to reflect.Type,
		data interface{},
	) (interface{}, error) {
		var t time.Duration

		if to == reflect.TypeOf(t) {
			// We might want to allow from int?
			if from == reflect.TypeOf("") {
				d, err := time.ParseDuration(data.(string))
				if err != nil {
					return nil, err
				}

				return d, nil
			} else {
				return nil, fmt.Errorf("Expected duration to be a string, got invalid data %T", data)
			}
		}

		return data, nil
	}
}

type SyncConfig struct {
	Peer      string        `validate:"required"`
	Direction SyncDirection `validate:"required"`
	Interval  time.Duration `validate:"required"`
}

type MaintenanceConfig struct {
	Interval   time.Duration `validate:"required"`
	Retention  time.Duration `validate:"required"`
	Repository string        `validate:"required"`
}

func NewConfiguration() *Configuration {
	return &Configuration{}
}

func DefaultConfiguration() *Configuration {
	return &Configuration{}
}

// ParseConfig parses the YAML file into the Config struct.
func ParseConfigFile(filename string) (*Configuration, error) {
	file := viper.New()
	file.SetConfigFile(filename)

	if err := file.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read configuration file: %w", err)
	}

	var config Configuration

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result: &config,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			BackupConfigCheckDecodeHook(),
			SyncDirectionDecodeHook(),
			DurationDecodeHook(),
		),
		ErrorUnused: true, // errors out if there are extra/unmapped keys
	})
	if err != nil {
		return nil, fmt.Errorf("creating decoder: %w", err)
	}

	if err := decoder.Decode(file.AllSettings()); err != nil {
		return nil, fmt.Errorf("decoding config: %w", err)
	}

	// Set default values for SyncConfig.Direction.
	for i := range config.Agent.Tasks {
		for j := range config.Agent.Tasks[i].Sync {
			if config.Agent.Tasks[i].Sync[j].Direction == "" {
				config.Agent.Tasks[i].Sync[j].Direction = SyncDirectionTo
			}
		}
	}

	validate := validator.New(validator.WithRequiredStructEnabled())

	validate.RegisterStructValidation(func(sl validator.StructLevel) {
		obj := sl.Current().Interface().(Task)
		if obj.Backup == nil && len(obj.Check) == 0 && len(obj.Restore) == 0 && len(obj.Sync) == 0 {
			sl.ReportError(obj, "Task", "Task", "atleastone", "at least one of Backup, Check, Restore, or Sync must be set")
		}
	}, Task{})

	if err := validate.Struct(config); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &config, nil
}
