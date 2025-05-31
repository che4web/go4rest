package go4rest

import (
	"gorm.io/gorm"
	"strings"
	"fmt"
	"net/url"
	"strconv"
)

type FilterOptions struct {
	Field    string
	Value    interface{}
	Operator string // "eq", "ne", "gt", "lt", "gte", "lte", "like"
}

func ApplyFilters(db *gorm.DB, filters []FilterOptions) *gorm.DB {
	for _, filter := range filters {
		switch filter.Operator {
		case "eq":
			db = db.Where(fmt.Sprintf("%s = ?", filter.Field), filter.Value)
			fmt.Printf("%s = ?", filter.Field,filter.Value)
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

// ParseQueryParams преобразует query-параметры в фильтры
func ParseQueryParams(params url.Values) []FilterOptions {
	var filters []FilterOptions

	for key, values := range params {
		// Берем первое значение (игнорируем multiple values)

		value := values[0]
		fmt.Printf(" all %+v %+v\n",key,value)
		if key=="page"{
			continue
		}
		if len(value) == 0 || value == "" {
			fmt.Printf(" continue %+v %+v\n",key,value)
			continue
		}
		if len(values)>1{
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
			if parts[1]=="like"{
				processedValue=value
			}else{
				processedValue = tryParseValue(value)
			}
			filters = append(filters, FilterOptions{
				Field:    parts[0],
				Value:    processedValue,
				Operator: parts[1],
			})
		}
	}

	return filters
}

// tryParseValue пытается преобразовать строку в соответствующий тип
func tryParseValue(value string) interface{} {
	// Пробуем bool
	if b, err := strconv.ParseBool(value); err == nil {
		return b
	}
	
	// Пробуем int
	if i, err := strconv.Atoi(value); err == nil {
		return i
	}
	
	// Пробуем float
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
	}
	
	// Возвращаем как строку
	return value
}
