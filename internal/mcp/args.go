package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// LLM-клиенты регулярно присылают скаляры в кавычках: "top": "10" вместо "top": 10.
// Строгий json.Unmarshal на этом падает, и вызов инструмента сгорает впустую — модель видит
// ошибку, переспрашивает и повторяет тот же вызов уже числом. Пользователь получает ответ,
// но платит лишним round-trip'ом.
//
// Чинить это в unstringifyJSON нельзя: та работает с сырым map[string]any, не зная целевых
// типов, и слепое превращение "123" в число сломало бы строковые поля — артикул или запрос
// вида "123" перестали бы разбираться. Поэтому терпимость живёт на типе поля.

// flexInt — целое, допускающее строковую форму на входе. Наружу это обычный int.
type flexInt int

func (f *flexInt) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "null" {
		*f = 0
		return nil
	}

	// Строковая форма: снимаем кавычки и разбираем содержимое.
	if strings.HasPrefix(raw, `"`) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		raw = strings.TrimSpace(s)
		if raw == "" {
			*f = 0
			return nil
		}
	}

	// ParseFloat, а не ParseInt: модели иногда шлют 10.0 — терять на этом вызов глупо.
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fmt.Errorf("expected an integer, got %s", string(data))
	}
	*f = flexInt(v)
	return nil
}

// flexFloat — дробное число, допускающее строковую форму. Нужно там, где величина по смыслу
// не целая: qty в спецификациях (0.5 единицы сырья на изделие — норма, а не опечатка).
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "null" {
		*f = 0
		return nil
	}

	if strings.HasPrefix(raw, `"`) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		raw = strings.TrimSpace(s)
		if raw == "" {
			*f = 0
			return nil
		}
	}

	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fmt.Errorf("expected a number, got %s", string(data))
	}
	*f = flexFloat(v)
	return nil
}

// flexBool — булево, допускающее строковую форму ("true", "1") и число (0/1).
type flexBool bool

func (f *flexBool) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "null" {
		*f = false
		return nil
	}

	if strings.HasPrefix(raw, `"`) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		raw = strings.TrimSpace(s)
		if raw == "" {
			*f = false
			return nil
		}
	}

	switch strings.ToLower(raw) {
	case "true", "1":
		*f = true
	case "false", "0":
		*f = false
	default:
		return fmt.Errorf("expected a boolean, got %s", string(data))
	}
	return nil
}

// clampLimit — размер выдачи resolve_*: 0/отрицательное и всё сверх resolve_limit
// схлопывается в resolve_limit.
func (h *Handler) clampLimit(limit flexInt) int {
	v := int(limit)
	if v <= 0 || v > h.cfg.Limits.ResolveLimit {
		return h.cfg.Limits.ResolveLimit
	}
	return v
}

// clampTop — число строк отчёта: 0/отрицательное означает «без явного ограничения»,
// то есть max_rows; всё сверх max_rows тоже режется до max_rows.
func (h *Handler) clampTop(top flexInt) int {
	return h.clampTopDefault(top, h.cfg.Limits.MaxRows)
}

// clampTopDefault — то же, но с собственным значением по умолчанию. Нужно для top_products,
// где «сколько-нибудь» разумнее трактовать как 10, а не как весь max_rows.
//
// max_rows — потолок и для def: иначе вызывающий с завышенным дефолтом протаскивал бы мимо
// лимита ровно то, ради чего лимит и заведён.
func (h *Handler) clampTopDefault(top flexInt, def int) int {
	if def > h.cfg.Limits.MaxRows {
		def = h.cfg.Limits.MaxRows
	}
	v := int(top)
	if v <= 0 {
		return def
	}
	if v > h.cfg.Limits.MaxRows {
		return h.cfg.Limits.MaxRows
	}
	return v
}
