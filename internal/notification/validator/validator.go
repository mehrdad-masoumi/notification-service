package notificationvalidator

import (
	"fmt"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
	"github.com/google/uuid"

	notificationdto "notification-service/internal/notification/dto"
	"notification-service/internal/notification/entity"
)

var allowedChannels = map[string]struct{}{
	string(entity.ChannelInApp):    {},
	string(entity.ChannelEmail):    {},
	string(entity.ChannelSMS):      {},
	string(entity.ChannelWhatsApp): {},
	string(entity.ChannelPush):     {},
}

var allowedPriorities = map[string]struct{}{
	string(entity.PriorityHigh):   {},
	string(entity.PriorityNormal): {},
	string(entity.PriorityLow):    {},
}

type Validator struct{}

func New() Validator {
	return Validator{}
}

func (v Validator) ValidateAdminCreate(req notificationdto.AdminCreateRequest) (map[string]string, error) {
	fields := map[string]string{}

	if err := validation.Validate(req.Title, validation.Required, validation.Length(1, 255)); err != nil {
		fields["title"] = "validation.required.title"
	}
	if err := validation.Validate(req.Message, validation.Required, validation.Length(1, 10000)); err != nil {
		fields["message"] = "validation.required.message"
	}
	if len(req.UserIDs) == 0 {
		fields["user_ids"] = "validation.required.user_ids"
	} else {
		for i, id := range req.UserIDs {
			if _, err := uuid.Parse(id); err != nil {
				fields[fmt.Sprintf("user_ids[%d]", i)] = "validation.invalid.uuid"
			}
		}
	}
	if errs := validateChannels(req.Channels); len(errs) > 0 {
		for k, val := range errs {
			fields[k] = val
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

func (v Validator) ValidateInternalCreate(req notificationdto.InternalCreateRequest) (map[string]string, error) {
	fields := map[string]string{}

	if err := validation.Validate(req.IdempotencyKey, validation.Required, validation.Length(1, 255)); err != nil {
		fields["idempotency_key"] = "validation.required.idempotency_key"
	}
	if err := validation.Validate(req.UserID, validation.Required, is.UUID); err != nil {
		fields["user_id"] = "validation.invalid.uuid"
	}
	if err := validation.Validate(req.Title, validation.Required, validation.Length(1, 255)); err != nil {
		fields["title"] = "validation.required.title"
	}
	if err := validation.Validate(req.Message, validation.Required, validation.Length(1, 10000)); err != nil {
		fields["message"] = "validation.required.message"
	}
	if errs := validateChannels(req.Channels); len(errs) > 0 {
		for k, val := range errs {
			fields[k] = val
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

func validateChannels(channels []string) map[string]string {
	fields := map[string]string{}
	if len(channels) == 0 {
		fields["channels"] = "validation.required.channels"
		return fields
	}
	for i, ch := range channels {
		if _, ok := allowedChannels[ch]; !ok {
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
