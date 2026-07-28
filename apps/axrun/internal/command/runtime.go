package command

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/cofy-x/axern/apps/axrun/internal/application/exportdata"
	approllout "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
	validateapp "github.com/cofy-x/axern/apps/axrun/internal/application/validate"
	axernbackend "github.com/cofy-x/axern/apps/axrun/internal/backend/axern"
	"github.com/cofy-x/axern/lib/go/clientconfig"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Options struct {
	ConfigPath  string
	ContextName string
	Output      string
}

type UsageError struct{ Err error }

func (e UsageError) Error() string { return e.Err.Error() }
func (e UsageError) Unwrap() error { return e.Err }

func Usage(err error) error { return UsageError{Err: err} }

func (o *Options) ResolveContext() (*clientconfig.Context, error) {
	_, value, ok, err := clientconfig.Resolve(o.ConfigPath, o.ContextName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return value, nil
}

func AxernConfig(value *clientconfig.Context) *axernbackend.Config {
	return &axernbackend.Config{
		Endpoint:      value.Endpoint,
		TLSCACert:     value.TLS.CACert,
		TLSCert:       value.TLS.Cert,
		TLSKey:        value.TLS.Key,
		TLSServerName: value.TLS.ServerName,
		ProxyMode:     value.ProxyMode,
	}
}

func PrintRollout(writer io.Writer, format string, result approllout.Result) error {
	return PrintValue(writer, format, result, "run=%s status=%s tasks=%d episodes=%d dir=%s\n", result.RunID, result.Status, result.TaskCount, result.EpisodeCount, result.RunDir)
}

func PrintValue(writer io.Writer, format string, value any, table string, args ...any) error {
	switch format {
	case "table", "":
		_, err := fmt.Fprintf(writer, table, args...)
		return err
	case "json":
		value = jsonDTO(value)
		if message, ok := value.(proto.Message); ok {
			data, err := (protojson.MarshalOptions{Indent: "  "}).Marshal(message)
			if err != nil {
				return err
			}
			data = append(data, '\n')
			_, err = writer.Write(data)
			return err
		}
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	default:
		return Usage(fmt.Errorf("output must be table or json"))
	}
}

// PrintJSONLine writes one compact JSON value followed by a newline. It is the
// output contract for commands that can emit more than one value over time.
// Keep PrintValue for non-streaming commands, where indented single-document
// JSON remains easier for humans to read.
func PrintJSONLine(writer io.Writer, value any) error {
	value = jsonDTO(value)
	if message, ok := value.(proto.Message); ok {
		data, err := (protojson.MarshalOptions{}).Marshal(message)
		if err != nil {
			return err
		}
		data = append(data, '\n')
		_, err = writer.Write(data)
		return err
	}
	return json.NewEncoder(writer).Encode(value)
}

func jsonDTO(value any) any {
	switch result := value.(type) {
	case approllout.Result:
		return map[string]any{
			"run_id": result.RunID, "status": result.Status, "run_dir": result.RunDir,
			"run_json": result.RunJSONPath, "plan_json": result.PlanJSONPath,
			"task_count": result.TaskCount, "episode_count": result.EpisodeCount,
			"attempts_per_task": result.AttemptsPerTask, "summary": result.Summary,
		}
	case exportdata.Result:
		return map[string]any{"format": result.Format, "run_id": result.RunID, "output_path": result.OutputPath, "record_count": result.RecordCount}
	case validateapp.Result:
		return map[string]any{"run_id": result.RunID, "run_dir": result.RunDir, "valid": result.Valid(), "problems": result.Problems}
	default:
		return value
	}
}

func ExactArgs(count int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(count)(cmd, args); err != nil {
			return Usage(err)
		}
		return nil
	}
}

func NoArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return Usage(err)
	}
	return nil
}

func FlagString(cmd *cobra.Command, name, fallback string) string {
	value, _ := cmd.Flags().GetString(name)
	if value == "" {
		return fallback
	}
	return value
}

func FlagInt(cmd *cobra.Command, name string, fallback int) int {
	value, _ := cmd.Flags().GetInt(name)
	if value == 0 {
		return fallback
	}
	return value
}
