package eventlog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/joeshaw/multierror"

	"github.com/elastic/beats/libbeat/common"
)

var commonConfigKeys = []string{"api", "name", "fields", "fields_under_root", "tags"}

// ConfigCommon is the common configuration data used to instantiate a new
// EventLog. Each implementation is free to support additional configuration
// options.
type ConfigCommon struct {
	API  string `config:"api"`  // Name of the API to use. Optional.
	Name string `config:"name"` // Name of the event log or channel.
}

type validator interface {
	Validate() error
}

func readConfig(
	c *common.Config,
	config interface{},
	validKeys []string,
) error {
	if err := c.Unpack(config); err != nil {
		return fmt.Errorf("Failed unpacking config. %v", err)
	}

	var errs multierror.Errors
	if len(validKeys) > 0 {
		sort.Strings(validKeys)

		// Check for invalid keys.
		for _, k := range c.GetFields() {
			k = strings.ToLower(k)
			i := sort.SearchStrings(validKeys, k)
			if i >= len(validKeys) || validKeys[i] != k {
				errs = append(errs, fmt.Errorf("Invalid event log key '%s' "+
					"found. Valid keys are %s", k, strings.Join(validKeys, ", ")))
			}
		}
	}

	if v, ok := config.(validator); ok {
		if err := v.Validate(); err != nil {
			errs = append(errs, err)
		}
	}

	return errs.Err()
}

// Producer produces a new event log instance for reading event log records.
type producer func(*common.Config) (EventLog, error)

// Channels lists the available channels (event logs).
type channels func() ([]string, error)

// eventLogInfo is the registration info associated with an event log API.
type eventLogInfo struct {
	apiName  string
	priority int
	producer producer
	channels func() ([]string, error)
}

// eventLogs is a map of priorities to eventLogInfo. The lower numbers have
// higher priorities.
var eventLogs = make(map[int]eventLogInfo)

// Register registers an EventLog API. Only the APIs that are available for the
// runtime OS should be registered. Each API must have a unique priority.
func Register(apiName string, priority int, producer producer, channels channels) {
	info, exists := eventLogs[priority]
	if exists {
		panic(fmt.Sprintf("%s API is already registered with priority %d. "+
			"Cannot register %s", info.apiName, info.priority, apiName))
	}

	eventLogs[priority] = eventLogInfo{
		apiName:  apiName,
		priority: priority,
		producer: producer,
		channels: channels,
	}
}

// New creates and returns a new EventLog instance based on the given config
// and the registered EventLog producers.
func New(options *common.Config) (EventLog, error) {
	if len(eventLogs) == 0 {
		return nil, fmt.Errorf("No event log API is available on this system")
	}

	var config ConfigCommon
	if err := readConfig(options, &config, nil); err != nil {
		return nil, err
	}

	// A specific API is being requested (usually done for testing).
	if config.API != "" {
		for _, v := range eventLogs {
			debugf("Checking %s", v.apiName)
			if strings.EqualFold(v.apiName, config.API) {
				debugf("Using %s API for event log %s", v.apiName, config.Name)
				e, err := v.producer(options)
				return e, err
			}
		}

		return nil, fmt.Errorf("%s API is not available", config.API)
	}

	// Use the API with the highest priority.
	keys := make([]int, 0, len(eventLogs))
	for key := range eventLogs {
		keys = append(keys, key)
	}
	sort.Ints(keys)

	eventLog := eventLogs[keys[0]]
	debugf("Using highest priority API, %s, for event log %s",
		eventLog.apiName, config.Name)
	e, err := eventLog.producer(options)
	return e, err
}
