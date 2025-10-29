package go4rest

import (
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

var (
	matchFirstCap = regexp.MustCompile("(.)([A-Z][a-z]+)")
	matchAllCap   = regexp.MustCompile("([a-z0-9])([A-Z])")
)

func ToSnakeCase(str string) string {
	snake := matchFirstCap.ReplaceAllString(str, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}

type FilterOptions struct {
	Field    string
	Value    interface{}
	Operator string // "eq", "ne", "gt", "lt", "gte", "lte", "like"
}

type SortOptions struct {
	Field string
	Order string // "asc", "desc"
}
type QueryOptions struct {
	Filters []FilterOptions
	Sorts   []SortOptions
}

func hasField(s interface{}, fieldName string) bool {
	t := reflect.TypeOf(s)

	// Если передали указатель, получаем тип элемента
	//
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Проверяем, что это структура
	t = t.Elem()

	// Ищем поле по имени
	_, found := t.FieldByNameFunc(func(n string) bool {
		return ToSnakeCase(n) == fieldName
	})
	return found
}

func checkIdField(model interface{}, fieldName string) string {
	name := fmt.Sprintf("%s_id", fieldName)
	if hasField(model, name) {
		return fmt.Sprintf("%s_id", fieldName)
	} else {
		return fieldName
	}
}

func ApplyFilters(db *gorm.DB, filters []FilterOptions) *gorm.DB {
	for _, filter := range filters {
		fieldName := checkIdField(db.Statement.Model, filter.Field)
		switch filter.Operator {
		case "eq":
			db = db.Where(fmt.Sprintf("%s = ?", fieldName), filter.Value)
		case "ne":
			db = db.Where(fmt.Sprintf("%s != ?", filter.Field), filter.Value)
		case "gt":
			db = db.Where(fmt.Sprintf("%s > ?", filter.Field), filter.Value)
		case "lt":
			db = db.Where(fmt.Sprintf("%s < ?", filter.Field), filter.Value)
		case "gte":
			db = db.Where(fmt.Sprintf("%s >= ?", filter.Field), filter.Value)
		case "lte":
			db = db.Where(fmt.Sprintf("%s <= ?", filter.Field), filter.Value)
		case "like":
			db = db.Where(fmt.Sprintf("%s LIKE ?", filter.Field), "%"+filter.Value.(string)+"%")
		case "in":
			db = db.Where(fmt.Sprintf("%s in ?", filter.Field), filter.Value)
		}
	}
	return db
}

func ApplySorting(db *gorm.DB, sorts []SortOptions) *gorm.DB {
	for _, sort := range sorts {
		order := strings.ToUpper(sort.Order)
		if order != "ASC" && order != "DESC" {
			order = "ASC" // значение по умолчанию
		}
		db = db.Order(fmt.Sprintf("%s %s", sort.Field, order))
	}
	return db
}

// ParseQueryParams преобразует query-параметры в фильтры
func ParseQueryParams(params url.Values) QueryOptions {
	var filters []FilterOptions
	var sorts []SortOptions

	for key, values := range params {
		// Берем первое значение (игнорируем multiple values)
		if key == "ordering" {
			var ordering string
			var f string
			if values[0][0:1] == "-" {
				ordering = "DESC"
				f = values[0][1:]
			} else {
				ordering = "ASC"
				f = values[0]
			}
			sort := SortOptions{
				Order: ordering,
				Field: f,
			}
			sorts = append(sorts, sort)
			continue
		}

		value := values[0]
		fmt.Printf(" all %+v %+v\n", key, value)
		if key == "page" {
			continue
		}
		if len(value) == 0 || value == "" {
			fmt.Printf(" continue %+v %+v\n", key, value)
			continue
		}
		if len(values) > 1 {
			filters = append(filters, FilterOptions{
				Field:    key,
				Value:    values,
				Operator: "in",
			})
			continue
		}

		parts := strings.Split(key, "__")
		if len(parts) == 1 {
			processedValue := tryParseValue(value)
			filters = append(filters, FilterOptions{
				Field:    key,
				Value:    processedValue,
				Operator: "eq",
			})
		} else {
			var processedValue interface{}
			if parts[1] == "like" {
				processedValue = value
			} else {
				processedValue = tryParseValue(value)
			}
			filters = append(filters, FilterOptions{
				Field:    parts[0],
				Value:    processedValue,
				Operator: parts[1],
			})
		}
	}

	return QueryOptions{
		Filters: filters,
		Sorts:   sorts,
	}
}

// tryParseValue пытается преобразовать строку в соответствующий тип
func tryParseValue(value string) interface{} {
	// Пробуем bool

	// Пробуем int
	if i, err := strconv.Atoi(value); err == nil {
		return i
	}

	// Пробуем float
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
	}
	if b, err := strconv.ParseBool(value); err == nil {
		return b
	}

	// Возвращаем как строку
	return value
}
