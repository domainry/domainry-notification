package template

import templatemodel "github.com/domainry/domainry-notification/internal/domain/template/model"
import templaterepository "github.com/domainry/domainry-notification/internal/domain/template/repository"
import templatevalidation "github.com/domainry/domainry-notification/internal/domain/template/validation"

type Template = templatemodel.Template
type Fallback = templatemodel.Fallback
type Variable = templatemodel.Variable
type Content = templatemodel.Content
type ProviderTemplate = templatemodel.ProviderTemplate
type ProviderTemplateComponent = templatemodel.ProviderTemplateComponent
type Fact = templatemodel.Fact
type Action = templatemodel.Action
type Rendered = templatemodel.Rendered
type Record = templatemodel.Record
type Version = templatemodel.Version
type PublicationStatus = templatemodel.PublicationStatus
type PublicationRequest = templatemodel.PublicationRequest
type PublicationTransition = templatemodel.PublicationTransition
type Store = templaterepository.Store
type RevisionStore = templaterepository.RevisionStore
type Capability = templatevalidation.Capability
type ProviderTemplateValidation = templatevalidation.ProviderTemplateValidation
type Provider = templatevalidation.Provider
type Capabilities = templatevalidation.Capabilities
type Validator = templatevalidation.Validator

var (
	ErrRecordConflict      = templaterepository.ErrRecordConflict
	ErrRecordNotFound      = templaterepository.ErrRecordNotFound
	ErrPublicationConflict = templaterepository.ErrPublicationConflict
	ErrPublicationNotFound = templaterepository.ErrPublicationNotFound
)

var NewCapabilities = templatevalidation.NewCapabilities
var NewValidator = templatevalidation.NewValidator
var ContentHash = templatevalidation.ContentHash
var ValueHash = templatevalidation.ValueHash

const (
	PublicationPending    = templatemodel.PublicationPending
	PublicationScheduled  = templatemodel.PublicationScheduled
	PublicationPublishing = templatemodel.PublicationPublishing
	PublicationPublished  = templatemodel.PublicationPublished
	PublicationRejected   = templatemodel.PublicationRejected
	PublicationCancelled  = templatemodel.PublicationCancelled
	PublicationFailed     = templatemodel.PublicationFailed
	PublicationSuperseded = templatemodel.PublicationSuperseded
)
