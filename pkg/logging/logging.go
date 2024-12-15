/*
 * Copyright © 2023 Christian Fritz <mail@chr-fritz.de>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package logging

import (
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// LevelFlagName defines the name of the cli parameter that configures the minimal printed log level.
var LevelFlagName = "log_level"

// FormatterFlagName defines the name of the cli parameter that configures the logging format (either structured text or
// json)
var FormatterFlagName = "log_format"

const LevelTrace = slog.LevelDebug - 1

// LoggerConfiguration encapsulates the configuration of the slog logger through command line arguments or viper
// configuration options.
type LoggerConfiguration interface {
	// Initialize creates a new slog.Logger, configures them and configures them as default logger.
	Initialize()
}

type loggerConfig struct {
	level         string
	formatterName string
	configLogger  *slog.Logger
}

// InitFlags initializes the appropriate logger command line flags on the given FlagSet and configures the
// autocompletion for them at the given cobra Command. It returns a LoggerConfiguration which encapsulates the later
// configuration of the slog logger.
func InitFlags(flagset *pflag.FlagSet, cmd *cobra.Command) LoggerConfiguration {
	if flagset == nil {
		flagset = pflag.CommandLine
	}
	config := &loggerConfig{
		configLogger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	flagset.StringVarP(&config.level, LevelFlagName, "v", "info", "The minimum log level to print the messages.")
	flagset.StringVarP(&config.formatterName, FormatterFlagName, "", "text", "The format how to print the log messages.")

	if cmd != nil {
		if e := cmd.RegisterFlagCompletionFunc(LevelFlagName, flagCompletion); e != nil {
			config.configLogger.Error("can not register flag completion for log_level", "error", e)
		}

		e := cmd.RegisterFlagCompletionFunc(FormatterFlagName, func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return []string{"text", "json"}, cobra.ShellCompDirectiveDefault
		})
		if e != nil {
			config.configLogger.Error("can not register flag completion for log formatter: ", "error", e)
		}
	}

	return config
}

func flagCompletion(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"error", "warn", "warning", "info", "debug"}, cobra.ShellCompDirectiveDefault
}

// Initialize creates a new slog.Logger, configures them and configures them as default logger.
func (lc *loggerConfig) Initialize() {
	handler := lc.createHandler()
	logger := slog.New(handler)
	slog.SetDefault(logger)
}

// parseLevel parses the configured logging level from a string.
func (lc *loggerConfig) parseLevel() (slog.Level, error) {
	var level slog.Level

	if err := level.UnmarshalText([]byte(strings.ToUpper(lc.level))); err != nil {
		return slog.LevelInfo, err
	}
	return level, nil
}

// createHandler creates the slog.Handler which will be used for the new default slog logger.
func (lc *loggerConfig) createHandler() slog.Handler {
	level, err := lc.parseLevel()
	if err != nil {
		lc.configLogger.Error("can not parse log level", "error", err)
	}
	options := &slog.HandlerOptions{
		Level: level,
	}

	switch strings.ToLower(lc.formatterName) {
	case "json":
		return slog.NewJSONHandler(os.Stdout, options)
	case "text":
		fallthrough
	default:
		return slog.NewTextHandler(os.Stdout, options)
	}
}
