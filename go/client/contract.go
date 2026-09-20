package client

// The generated contract types this package's API reaches, re-exported so an
// application names them through this package and never imports the generated
// one. scripts/idiom_check.py refuses a reachable type this file leaves out.

import (
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
)

type ConfigObservationOutcome = wire.ConfigObservationOutcome

const (
	ConfigObservationOutcomeSnapshot    = wire.ConfigObservationOutcomeSnapshot
	ConfigObservationOutcomeUnchanged   = wire.ConfigObservationOutcomeUnchanged
	ConfigObservationOutcomeGap         = wire.ConfigObservationOutcomeGap
	ConfigObservationOutcomeUnavailable = wire.ConfigObservationOutcomeUnavailable
	ConfigObservationOutcomeUnsupported = wire.ConfigObservationOutcomeUnsupported
	ConfigObservationOutcomeInvalid     = wire.ConfigObservationOutcomeInvalid
)

// ConfigObservationOutcomeValues returns every member of ConfigObservationOutcome in declaration order, in a new slice.
func ConfigObservationOutcomeValues() []ConfigObservationOutcome {
	return wire.ConfigObservationOutcomeValues()
}

type Origin = wire.Origin

type Origins = wire.Origins

type ServiceError = wire.ServiceError

type ServiceErrorCode = wire.ServiceErrorCode

const (
	ServiceErrorCodeHandlerError       = wire.ServiceErrorCodeHandlerError
	ServiceErrorCodeInvalidResult      = wire.ServiceErrorCodeInvalidResult
	ServiceErrorCodeUnknownVersion     = wire.ServiceErrorCodeUnknownVersion
	ServiceErrorCodeUnknownService     = wire.ServiceErrorCodeUnknownService
	ServiceErrorCodeUnknownMethod      = wire.ServiceErrorCodeUnknownMethod
	ServiceErrorCodeWrongMode          = wire.ServiceErrorCodeWrongMode
	ServiceErrorCodeStorageUnavailable = wire.ServiceErrorCodeStorageUnavailable
	ServiceErrorCodeCallerUnavailable  = wire.ServiceErrorCodeCallerUnavailable
	ServiceErrorCodeIdentityRequired   = wire.ServiceErrorCodeIdentityRequired
	ServiceErrorCodeWrongUser          = wire.ServiceErrorCodeWrongUser
	ServiceErrorCodeInvalidRevision    = wire.ServiceErrorCodeInvalidRevision
)

// ServiceErrorCodeValues returns every member of ServiceErrorCode in declaration order, in a new slice.
func ServiceErrorCodeValues() []ServiceErrorCode { return wire.ServiceErrorCodeValues() }

type UserReplaceOutcome = wire.UserReplaceOutcome

const (
	UserReplaceOutcomeApplied     = wire.UserReplaceOutcomeApplied
	UserReplaceOutcomeConflict    = wire.UserReplaceOutcomeConflict
	UserReplaceOutcomeForbidden   = wire.UserReplaceOutcomeForbidden
	UserReplaceOutcomeUnavailable = wire.UserReplaceOutcomeUnavailable
)

// UserReplaceOutcomeValues returns every member of UserReplaceOutcome in declaration order, in a new slice.
func UserReplaceOutcomeValues() []UserReplaceOutcome { return wire.UserReplaceOutcomeValues() }
