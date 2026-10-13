// Code generated from Pkl module `alexandria.ai.provider.Provider`. DO NOT EDIT.
package provider

import "github.com/bmurray/alexandria/internal/gen/credential"

type Provider interface {
	GetType() string

	GetName() string

	GetModel() string

	GetApiKey() credential.Credential

	GetInputCostPerMillionUsd() float64

	GetOutputCostPerMillionUsd() float64

	GetCacheReadCostPerMillionUsd() float64

	GetMaxTokens() uint32

	GetPriority() uint16

	GetSpendLimitUsd() *float64

	GetEnabled() bool

	GetTimeoutSeconds() uint16
}
