package cmd

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v2"
)

const (
	configFilePath                                      = "./config.yaml"
	defaultSlashingPeriodUptimeWarningThreshold float64 = 99.80 // 20 of the last 10,000 blocks missed
	defaultSlashingPeriodUptimeErrorThreshold   float64 = 98    // 200 of the last 10,000 blocks missed
	defaultRecentBlocksToCheck                  int64   = 20
	defaultNotifyEvery                          int64   = 20 // check runs every ~30 seconds, so will notify for continued errors and rollup stats every ~10 mins
	defaultRecentMissedBlocksNotifyThreshold    int64   = 10
	sentryGRPCErrorNotifyThreshold                      = 1 // will notify with error for any more than this number of consecutive grpc errors for a given sentry
	sentryOutOfSyncErrorNotifyThreshold                 = 1 // will notify with error for any more than this number of consecutive out of sync errors for a given sentry
	sentryHaltErrorNotifyThreshold                      = 1 // will notify with error for any more than this number of consecutive halt errors for a given sentry
)

type AlertLevel int8

const (
	alertLevelNone AlertLevel = iota
	alertLevelWarning
	alertLevelHigh
	alertLevelCritical
)

type AlertType string

const (
	alertTypeJailed             AlertType = "alertTypeJailed"
	alertTypeTombstoned         AlertType = "alertTypeTombstoned"
	alertTypeOutOfSync          AlertType = "alertTypeOutOfSync"
	alertTypeBlockFetch         AlertType = "alertTypeBlockFetch"
	alertTypeMissedRecentBlocks AlertType = "alertTypeMissedRecentBlocks"
	alertTypeGenericRPC         AlertType = "alertTypeGenericRPC"
	alertTypeHalt               AlertType = "alertTypeHalt"
	alertTypeSlashingSLA        AlertType = "alertTypeSlashingSLA"
)

var alertTypes = []AlertType{
	alertTypeJailed,
	alertTypeTombstoned,
	alertTypeOutOfSync,
	alertTypeBlockFetch,
	alertTypeMissedRecentBlocks,
	alertTypeGenericRPC,
	alertTypeHalt,
	alertTypeSlashingSLA,
}

func (at *AlertType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	alertType := ""
	err := unmarshal(&alertType)
	if err != nil {
		return err
	}

	found := false
	for _, s := range alertTypes {
		a := AlertType(alertType)
		if s == a {
			found = true
			*at = a
		}
	}

	if !found {
		return errors.New("Invalid AlertType")
	}

	return nil
}

type SentryAlertType int8

const (
	sentryAlertTypeNone SentryAlertType = iota
	sentryAlertTypeGRPCError
	sentryAlertTypeOutOfSyncError
	sentryAlertTypeHalt
)

type SentryStats struct {
	Name            string
	Version         string
	Height          int64
	SentryAlertType SentryAlertType
}

type ValidatorStats struct {
	Timestamp                   time.Time
	Height                      int64
	RecentMissedBlocks          int64
	LastSignedBlockHeight       int64
	RecentMissedBlockAlertLevel AlertLevel
	LastSignedBlockTimestamp    time.Time
	SlashingPeriodUptime        float64
	SentryStats                 []*SentryStats
	AlertLevel                  AlertLevel
	RPCError                    bool
}

type ValidatorAlertState struct {
	AlertTypeCounts              map[AlertType]int64
	SentryGRPCErrorCounts        map[string]int64
	SentryOutOfSyncErrorCounts   map[string]int64
	SentryHaltErrorCounts        map[string]int64
	SentryLatestHeight           map[string]int64
	RecentMissedBlocksCounter    int64
	RecentMissedBlocksCounterMax int64
	LatestBlockChecked           int64
	LatestBlockSigned            int64
}

type ValidatorAlertNotification struct {
	Alerts         []string
	ClearedAlerts  []string
	NotifyForClear bool
	AlertLevel     AlertLevel
}

type NotificationsConfig struct {
	Service string                `yaml:"service"`
	Discord *DiscordChannelConfig `yaml:"discord"`
}

type AlertConfig struct {
	IgnoreAlerts []*AlertType `yaml:"ignore-alerts"`
}

func (at *AlertConfig) AlertActive(alert AlertType) bool {
	for _, a := range at.IgnoreAlerts {
		if *a == alert {
			return false
		}
	}
	return true
}

type HalfLifeConfig struct {
	AlertConfig   AlertConfig          `yaml:"alerts"`
	Notifications *NotificationsConfig `yaml:"notifications"`
	Validators    []*ValidatorMonitor  `yaml:"validators"`
}

func (c *HalfLifeConfig) getUnsetDefaults() {
	fmt.Printf("%+v", *c.Notifications)
	for idx := range c.Validators {
		if c.Validators[idx].SlashingPeriodUptimeWarningThreshold == 0 {
			c.Validators[idx].SlashingPeriodUptimeWarningThreshold = defaultSlashingPeriodUptimeWarningThreshold
		}
		if c.Validators[idx].SlashingPeriodUptimeErrorThreshold == 0 {
			c.Validators[idx].SlashingPeriodUptimeErrorThreshold = defaultSlashingPeriodUptimeErrorThreshold
		}
		if c.Validators[idx].RecentBlocksToCheck == 0 {
			c.Validators[idx].RecentBlocksToCheck = defaultRecentBlocksToCheck
		}
		if c.Validators[idx].NotifyEvery == 0 {
			c.Validators[idx].NotifyEvery = defaultNotifyEvery
		}
		if c.Validators[idx].RecentMissedBlocksNotifyThreshold == 0 {
			c.Validators[idx].RecentMissedBlocksNotifyThreshold = defaultRecentMissedBlocksNotifyThreshold
		}
		if c.Validators[idx].MissedBlocksGreenTo == nil {
			defaultVal := int64(49)
			c.Validators[idx].MissedBlocksGreenTo = &defaultVal
		}
		if c.Validators[idx].MissedBlocksYellowFrom == nil {
			defaultVal := int64(50)
			c.Validators[idx].MissedBlocksYellowFrom = &defaultVal
		}
		if c.Validators[idx].MissedBlocksYellowTo == nil {
			defaultVal := int64(99)
			c.Validators[idx].MissedBlocksYellowTo = &defaultVal
		}
		if c.Validators[idx].MissedBlocksRedFrom == nil {
			defaultVal := int64(100)
			c.Validators[idx].MissedBlocksRedFrom = &defaultVal
		}
	}
}

type DiscordWebhookConfig struct {
	ID    string `yaml:"id"`
	Token string `yaml:"token"`
}

type DiscordChannelConfig struct {
	Webhook      DiscordWebhookConfig `yaml:"webhook"`
	AlertUserIDs []string             `yaml:"alert-user-ids"`
	Username     string               `yaml:"username"`
}

type Sentry struct {
	Name string `yaml:"name"`
	GRPC string `yaml:"grpc"`
}

type ValidatorMonitor struct {
	Name                           string    `yaml:"name"`
	RPC                            string    `yaml:"rpc"`
	FullNode                       bool      `yaml:"fullnode"`
	Address                        string    `yaml:"address"`
	ChainID                        string    `yaml:"chain-id"`
	DiscordStatusMessageID         *string   `yaml:"discord-status-message-id"`
	RPCRetries                     *int      `yaml:"rpc-retries"`
	MissedBlocksThreshold          *int64    `yaml:"missed-blocks-threshold"`
	SentryGRPCErrorThreshold       *int64    `yaml:"sentry-grpc-error-threshold"`
	SentryOutOfSyncBlocksThreshold *int64    `yaml:"sentry-out-of-sync-blocks-threshold"`
	Sentries                       *[]Sentry `yaml:"sentries"`

	SlashingPeriodUptimeWarningThreshold float64 `yaml:"slashing_warn_threshold"`
	SlashingPeriodUptimeErrorThreshold   float64 `yaml:"slashing_error_threshold"`
	RecentBlocksToCheck                  int64   `yaml:"recent_blocks_to_check"`
	NotifyEvery                          int64   `yaml:"notify_every"`
	RecentMissedBlocksNotifyThreshold    int64   `yaml:"recent_missed_blocks_notify_threshold"`

	MissedBlocksGreenTo    *int64 `yaml:"missed-blocks-green-to"`
	MissedBlocksYellowFrom *int64 `yaml:"missed-blocks-yellow-from"`
	MissedBlocksYellowTo   *int64 `yaml:"missed-blocks-yellow-to"`
	MissedBlocksRedFrom    *int64 `yaml:"missed-blocks-red-from"`
}

func saveConfig(configFile string, config *HalfLifeConfig, writeConfigMutex *sync.Mutex) {
	writeConfigMutex.Lock()
	defer writeConfigMutex.Unlock()

	yamlBytes, err := yaml.Marshal(config)
	if err != nil {
		fmt.Printf("Error during config yaml marshal %v\n", err)
	}

	err = os.WriteFile(configFile, yamlBytes, 0600)
	if err != nil {
		fmt.Printf("Error saving config yaml %v\n", err)
	}
}
