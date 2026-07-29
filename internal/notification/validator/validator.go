package notificationvalidator

import (
	"fmt"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"

	notificationdto "notification-service/internal/notification/dto"
	"notification-service/internal/notification/entity"
)

var allowedPriorities = map[string]struct{}{
	string(entity.PriorityHigh):   {},
	string(entity.PriorityNormal): {},
	string(entity.PriorityLow):    {},
}

// Validator validates notification requests against the set of channels
// currently enabled by configuration (config.Config.EnabledChannels()), so
// e.g. an unconfigured SMS provider is rejected at the API boundary rather
// than silently failing in the worker.
type Validator struct {
	enabledChannels map[string]bool
}

func New(enabledChannels map[string]bool) Validator {
	if enabledChannels == nil {
		enabledChannels = map[string]bool{}
	}
	return Validator{enabledChannels: enabledChannels}
}

// ValidateCommand validates the v1 template-driven command. Channels are
// optional here: when omitted, the service derives them from the
// templates registered for TemplateCode. Contacts is required (email/phone
// may be empty when only in_app/push are used).
func (v Validator) ValidateCommand(req notificationdto.CommandRequest) (map[string]string, error) {
	fields := map[string]string{}

	if err := validation.Validate(req.IdempotencyKey, validation.Required, validation.Length(1, 255)); err != nil {
		fields["idempotency_key"] = "validation.required.idempotency_key"
	}
	if err := validation.Validate(req.UserID, validation.Required, is.UUID); err != nil {
		fields["user_id"] = "validation.invalid.uuid"
	}
	if err := validation.Validate(req.TemplateCode, validation.Required, validation.Length(1, 100)); err != nil {
		fields["template_code"] = "validation.required.template_code"
	}
	if req.Contacts == nil {
		fields["contacts"] = "validation.required.contacts"
	} else {
		if req.Contacts.Email != "" {
			if err := validation.Validate(req.Contacts.Email, is.EmailFormat); err != nil {
				fields["contacts.email"] = "validation.invalid.email"
			}
		}
	}
	if len(req.Channels) > 0 {
		if errs := v.validateChannels(req.Channels, false); len(errs) > 0 {
			for k, val := range errs {
				fields[k] = val
			}
		}
	}
	if err := validatePriority(req.Priority); err != "" {
		fields["priority"] = err
	}

	if len(fields) > 0 {
		return fields, fmt.Errorf("validation failed")
	}
	return nil, nil
}

func (v Validator) validateChannels(channels []string, required bool) map[string]string {
	fields := map[string]string{}
	if len(channels) == 0 {
		if required {
			fields["channels"] = "validation.required.channels"
		}
		return fields
	}
	for i, ch := range channels {
		if !v.enabledChannels[ch] {
			fields[fmt.Sprintf("channels[%d]", i)] = "validation.invalid.channel"
		}
	}
	return fields
}

func validatePriority(p string) string {
	if p == "" {
		return ""
	}
	if _, ok := allowedPriorities[p]; !ok {
		return "validation.invalid.priority"
	}
	return ""
}
