package go4rest

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func queryColumn(db *gorm.DB, name string) (clause.Column, error) {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(db.Statement.Model); err != nil {
		return clause.Column{}, err
	}
	field := stmt.Schema.LookUpField(name)
	if field == nil {
		field = stmt.Schema.LookUpField(name + "_id")
	}
	if field == nil || field.DBName == "" || field.Tag.Get("json") == "-" {
		return clause.Column{}, fmt.Errorf("invalid query field %q", name)
	}
	return clause.Column{Table: clause.CurrentTable, Name: field.DBName}, nil
}

func ApplyFilters(db *gorm.DB, filters []FilterOptions) *gorm.DB {
	for _, filter := range filters {
		column, err := queryColumn(db, filter.Field)
		if err != nil {
			db.AddError(err)
			return db
		}
		switch filter.Operator {
		case "eq":
			db = db.Where(clause.Eq{Column: column, Value: filter.Value})
		case "ne":
			db = db.Where(clause.Neq{Column: column, Value: filter.Value})
		case "gt":
			db = db.Where(clause.Gt{Column: column, Value: filter.Value})
		case "lt":
			db = db.Where(clause.Lt{Column: column, Value: filter.Value})
		case "gte":
			db = db.Where(clause.Gte{Column: column, Value: filter.Value})
		case "lte":
			db = db.Where(clause.Lte{Column: column, Value: filter.Value})
		case "like":
			db = db.Where(clause.Like{Column: column, Value: "%" + fmt.Sprint(filter.Value) + "%"})
		case "in":
			db = db.Where(clause.Expr{SQL: "? IN (?)", Vars: []interface{}{column, filter.Value}})
		case "isnull":
			if b, ok := filter.Value.(bool); ok && b {
				db = db.Where(clause.Eq{Column: column, Value: nil})
			} else {
				db = db.Where(clause.Neq{Column: column, Value: nil})
			}
		}
	}
	return db
}

func ApplySorting(db *gorm.DB, sorts []SortOptions) *gorm.DB {
	for _, sort := range sorts {
		column, err := queryColumn(db, sort.Field)
		if err != nil {
			db.AddError(err)
			return db
		}
		order := strings.ToUpper(sort.Order)
		if order != "ASC" && order != "DESC" {
			order = "ASC" // значение по умолчанию
		}
		db = db.Order(clause.OrderByColumn{Column: column, Desc: order == "DESC"})
	}
	return db
}

// ParseQueryParams преобразует query-параметры в фильтры
func ParseQueryParams(params url.Values) QueryOptions {
	var filters []FilterOptions
	var sorts []SortOptions

	for key, values := range params {
		if len(values) == 0 || values[0] == "" {
			continue
		}
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
		if key == "page" {
			continue
		}

		if key == "per_page" {
			continue
		}
		if len(value) == 0 || value == "" {
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
