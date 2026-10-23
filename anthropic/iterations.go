package anthropic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

const (
	advisorPass    = "advisor_message"
	compactionPass = "compaction"
)

const maxIdempotencyKeyLength = 255

type billedPass struct {
	index               int
	kind                string
	model               string
	inputTokens         int64
	outputTokens        int64
	cacheCreationTokens int64
	cacheReadTokens     int64
	cacheWrites         cacheWriteSplit
}

type billedPassReporter interface {
	reportedBilledPasses() []billedPass
}

func billedPassesOf(iterations anthropic.BetaIterationsUsage) []billedPass {
	var passes []billedPass
	for index, entry := range iterations {
		if entry.Type != advisorPass && entry.Type != compactionPass {
			continue
		}
		passes = append(passes, billedPass{
			index:               index,
			kind:                entry.Type,
			model:               string(entry.Model),
			inputTokens:         entry.InputTokens,
			outputTokens:        entry.OutputTokens,
			cacheCreationTokens: entry.CacheCreationInputTokens,
			cacheReadTokens:     entry.CacheReadInputTokens,
			cacheWrites:         betaCacheWriteSplitOf(entry.CacheCreation),
		})
	}
	return passes
}

func billedPassesInUsage(usageJSON string) (passes []billedPass, reported bool) {
	var usage struct {
		Iterations *anthropic.BetaIterationsUsage `json:"iterations"`
	}
	if usageJSON == "" || json.Unmarshal([]byte(usageJSON), &usage) != nil || usage.Iterations == nil {
		return nil, false
	}
	return billedPassesOf(*usage.Iterations), true
}

func billedPassesInDecodedUsage(usage map[string]interface{}) ([]billedPass, bool) {
	encoded, err := json.Marshal(usage)
	if err != nil {
		return nil, false
	}
	return billedPassesInUsage(string(encoded))
}

func advisorToolModel(tools []anthropic.BetaToolUnionParam) string {
	for _, tool := range tools {
		if tool.OfAdvisorTool20260301 != nil {
			return string(tool.OfAdvisorTool20260301.Model)
		}
	}
	return ""
}

func iterationTransactionID(mainID string, index int) string {
	return fmt.Sprintf("%s-iteration-%d", mainID, index)
}

func iterationIdempotencyKey(mainKey string, index int) string {
	derived := iterationTransactionID(mainKey, index)
	if len(derived) <= maxIdempotencyKeyLength {
		return derived
	}
	digest := sha256.Sum256([]byte(mainKey))
	return iterationTransactionID(hex.EncodeToString(digest[:]), index)
}

func (p billedPass) recordedModel(main *metering.MeteringPayload, advisorModel string) string {
	if p.kind != advisorPass {
		return main.Model
	}
	for _, candidate := range []string{p.model, advisorModel} {
		if candidate != "" {
			return candidate
		}
	}
	return main.Model
}

func passIdempotencyKey(mainKey, passTransactionID string, index int) string {
	if mainKey == "" {
		return passTransactionID
	}
	return iterationIdempotencyKey(mainKey, index)
}

func deepCopied[T any](value T) T {
	copier := deepCopier{copies: map[visitedValue]reflect.Value{}}
	return copier.copy(reflect.ValueOf(&value).Elem()).Interface().(T)
}

type visitedValue struct {
	typ     reflect.Type
	address uintptr
	length  int
}

// deepCopier copies caller-supplied metadata, which may refer back to itself:
// a pointer, map or slice seen before is answered with its copy, so a cycle
// is reproduced in the copy instead of being followed forever.
type deepCopier struct {
	copies map[visitedValue]reflect.Value
}

func (c *deepCopier) copy(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Ptr:
		if value.IsNil() {
			return value
		}
		return c.once(value, 0, func() reflect.Value { return reflect.New(value.Type().Elem()) }, func(copied reflect.Value) {
			copied.Elem().Set(c.copy(value.Elem()))
		})
	case reflect.Map:
		if value.IsNil() {
			return value
		}
		return c.once(value, 0, func() reflect.Value { return reflect.MakeMapWithSize(value.Type(), value.Len()) }, func(copied reflect.Value) {
			for iter := value.MapRange(); iter.Next(); {
				copied.SetMapIndex(iter.Key(), c.copy(iter.Value()))
			}
		})
	case reflect.Slice:
		if value.IsNil() || value.Len() == 0 {
			return value
		}
		return c.once(value, value.Len(), func() reflect.Value { return reflect.MakeSlice(value.Type(), value.Len(), value.Len()) }, func(copied reflect.Value) {
			for i := 0; i < value.Len(); i++ {
				copied.Index(i).Set(c.copy(value.Index(i)))
			}
		})
	case reflect.Interface:
		if value.IsNil() {
			return value
		}
		copied := reflect.New(value.Type()).Elem()
		copied.Set(c.copy(value.Elem()))
		return copied
	case reflect.Array:
		copied := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			copied.Index(i).Set(c.copy(value.Index(i)))
		}
		return copied
	case reflect.Struct:
		copied := reflect.New(value.Type()).Elem()
		copied.Set(value)
		for i := 0; i < value.NumField(); i++ {
			if copied.Field(i).CanSet() {
				copied.Field(i).Set(c.copy(value.Field(i)))
			}
		}
		return copied
	default:
		return value
	}
}

func (c *deepCopier) once(value reflect.Value, length int, allocate func() reflect.Value, fill func(reflect.Value)) reflect.Value {
	key := visitedValue{typ: value.Type(), address: value.Pointer(), length: length}
	if copied, seen := c.copies[key]; seen {
		return copied
	}
	copied := allocate()
	c.copies[key] = copied
	fill(copied)
	return copied
}

// A pass record carries who and what the call belongs to, its timing and its
// request settings, and nothing that prices or describes the call's own
// response, so none of that is counted once per pass.
func iterationRecord(main *metering.MeteringPayload, pass billedPass, advisorModel string) *metering.MeteringPayload {
	transactionID := iterationTransactionID(main.TransactionID, pass.index)
	return &metering.MeteringPayload{
		IdempotencyKey:      passIdempotencyKey(main.IdempotencyKey, transactionID, pass.index),
		TransactionID:       transactionID,
		ParentTransactionID: main.TransactionID,
		// The payload builder drops subtypes on chat records; Revenium stores these two.
		OperationSubtype:          pass.kind,
		Model:                     pass.recordedModel(main, advisorModel),
		InputTokenCount:           pass.inputTokens,
		OutputTokenCount:          pass.outputTokens,
		TotalTokenCount:           pass.inputTokens + pass.outputTokens,
		CacheCreationTokenCount:   pass.cacheCreationTokens,
		CacheReadTokenCount:       pass.cacheReadTokens,
		CacheCreation5mTokenCount: deepCopied(pass.cacheWrites.fiveMinute),
		CacheCreation1hTokenCount: deepCopied(pass.cacheWrites.oneHour),
		StopReason:                "END",

		CostType:            main.CostType,
		IsStreamed:          main.IsStreamed,
		OperationType:       main.OperationType,
		Provider:            main.Provider,
		MiddlewareSource:    main.MiddlewareSource,
		ModelSource:         main.ModelSource,
		RequestTime:         main.RequestTime,
		ResponseTime:        main.ResponseTime,
		RequestDuration:     main.RequestDuration,
		CompletionStartTime: main.CompletionStartTime,
		TimeToFirstToken:    main.TimeToFirstToken,
		SystemFingerprint:   main.SystemFingerprint,
		Temperature:         deepCopied(main.Temperature),
		Attributes:          deepCopied(main.Attributes),

		OrganizationName:           main.OrganizationName,
		ProductName:                main.ProductName,
		TaskType:                   main.TaskType,
		Agent:                      main.Agent,
		AgentVersion:               main.AgentVersion,
		SquadID:                    main.SquadID,
		SquadName:                  main.SquadName,
		SquadRole:                  main.SquadRole,
		SubscriptionID:             main.SubscriptionID,
		TraceID:                    main.TraceID,
		TraceType:                  main.TraceType,
		TraceName:                  main.TraceName,
		TransactionName:            main.TransactionName,
		TicketID:                   main.TicketID,
		AgenticJobID:               main.AgenticJobID,
		AgenticJobName:             main.AgenticJobName,
		AgenticJobType:             main.AgenticJobType,
		AgenticJobVersion:          main.AgenticJobVersion,
		Environment:                main.Environment,
		Region:                     main.Region,
		RetryNumber:                deepCopied(main.RetryNumber),
		CredentialAlias:            main.CredentialAlias,
		Subscriber:                 deepCopied(main.Subscriber),
		TaskID:                     main.TaskID,
		CodingAssistantAccountUUID: main.CodingAssistantAccountUUID,

		Effort:                 main.Effort,
		SpeedMode:              main.SpeedMode,
		QuerySource:            main.QuerySource,
		SubagentType:           main.SubagentType,
		PromptID:               main.PromptID,
		PromptLength:           deepCopied(main.PromptLength),
		ClientSurface:          main.ClientSurface,
		SkillName:              main.SkillName,
		SkillInvocationTrigger: main.SkillInvocationTrigger,
		SkillSource:            main.SkillSource,
		SkillKind:              main.SkillKind,
		SkillPluginName:        main.SkillPluginName,
		SkillMarketplaceName:   main.SkillMarketplaceName,
	}
}

func sendTurnRecords(mc *metering.MeteringClient, main *metering.MeteringPayload, passes []billedPass, advisorModel string) {
	mc.Send(main)
	for _, pass := range passes {
		if record, err := guardedIterationRecord(main, pass, advisorModel); err != nil {
			core.Warn("[METERING] the %s pass of transaction %s was not recorded: %v", pass.kind, main.TransactionID, err)
		} else {
			mc.Send(record)
		}
	}
}

// guardedIterationRecord keeps a panic while copying caller-supplied metadata
// by reflection from reaching the caller's request; the call's own record is
// already sent.
func guardedIterationRecord(main *metering.MeteringPayload, pass billedPass, advisorModel string) (record *metering.MeteringPayload, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("copying the call's metadata failed: %v", recovered)
		}
	}()
	return iterationRecord(main, pass, advisorModel), nil
}
