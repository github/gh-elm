package target

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func resolveTargetMigrationID(positional string, flagValue int64, flagChanged bool) (int64, error) {
	if positional != "" && flagChanged {
		return 0, errors.New("TARGET-MIGRATION-ID cannot be provided both positionally and with --migration-id")
	}
	if positional != "" {
		value, err := strconv.ParseInt(positional, 10, 64)
		if err != nil || value <= 0 {
			return 0, fmt.Errorf("invalid TARGET-MIGRATION-ID %q: must be a positive integer", positional)
		}
		return value, nil
	}
	if !flagChanged || flagValue <= 0 {
		return 0, errors.New("TARGET-MIGRATION-ID is required")
	}
	return flagValue, nil
}

func resolveStringOperand(name, positional, flagName, flagValue string, flagChanged bool) (string, error) {
	positional = strings.TrimSpace(positional)
	flagValue = strings.TrimSpace(flagValue)
	if positional != "" && flagChanged {
		return "", fmt.Errorf("%s cannot be provided both positionally and with --%s", name, flagName)
	}
	if positional != "" {
		return positional, nil
	}
	if flagValue != "" {
		return flagValue, nil
	}
	return "", fmt.Errorf("%s is required", name)
}

func resolveAliasedFlag(primaryName, primaryValue string, primaryChanged bool, legacyName, legacyValue string, legacyChanged bool) (string, string, bool, error) {
	if primaryChanged && legacyChanged {
		return "", "", false, fmt.Errorf("--%s and --%s cannot be used together", primaryName, legacyName)
	}
	if primaryChanged {
		return primaryValue, primaryName, true, nil
	}
	if legacyChanged {
		return legacyValue, legacyName, true, nil
	}
	return "", primaryName, false, nil
}
