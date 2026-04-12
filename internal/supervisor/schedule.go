package supervisor

import (
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

type cronSchedule struct {
	minutes  fieldMatcher
	hours    fieldMatcher
	days     fieldMatcher
	months   fieldMatcher
	weekdays fieldMatcher
}

type fieldMatcher map[int]struct{}

func nextCronTime(expression string, after time.Time) (time.Time, error) {
	schedule, err := parseCronSchedule(expression)
	if err != nil {
		return time.Time{}, err
	}

	current := after.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := current.AddDate(5, 0, 0)
	for !current.After(limit) {
		if schedule.matches(current) {
			return current, nil
		}
		current = current.Add(time.Minute)
	}
	return time.Time{}, errors.New("could not find a matching cron execution time")
}

func parseCronSchedule(expression string) (*cronSchedule, error) {
	parts := strings.Fields(expression)
	if len(parts) != 5 {
		return nil, errors.New("cron schedule must have 5 fields: minute hour day month weekday")
	}

	minutes, err := parseField(parts[0], 0, 59)
	if err != nil {
		return nil, errors.Wrap(err, "parse minute field")
	}
	hours, err := parseField(parts[1], 0, 23)
	if err != nil {
		return nil, errors.Wrap(err, "parse hour field")
	}
	days, err := parseField(parts[2], 1, 31)
	if err != nil {
		return nil, errors.Wrap(err, "parse day field")
	}
	months, err := parseField(parts[3], 1, 12)
	if err != nil {
		return nil, errors.Wrap(err, "parse month field")
	}
	weekdays, err := parseField(parts[4], 0, 6)
	if err != nil {
		return nil, errors.Wrap(err, "parse weekday field")
	}

	return &cronSchedule{
		minutes:  minutes,
		hours:    hours,
		days:     days,
		months:   months,
		weekdays: weekdays,
	}, nil
}

func (s *cronSchedule) matches(t time.Time) bool {
	return s.minutes.match(t.Minute()) &&
		s.hours.match(t.Hour()) &&
		s.days.match(t.Day()) &&
		s.months.match(int(t.Month())) &&
		s.weekdays.match(int(t.Weekday()))
}

func (m fieldMatcher) match(value int) bool {
	_, ok := m[value]
	return ok
}

func parseField(raw string, min, max int) (fieldMatcher, error) {
	values := make(fieldMatcher)
	for _, segment := range strings.Split(raw, ",") {
		if err := addSegment(values, strings.TrimSpace(segment), min, max); err != nil {
			return nil, err
		}
	}
	if len(values) == 0 {
		return nil, errors.New("field cannot be empty")
	}
	return values, nil
}

func addSegment(values fieldMatcher, segment string, min, max int) error {
	if segment == "" {
		return errors.New("empty segment")
	}

	base := segment
	step := 1
	if strings.Contains(segment, "/") {
		parts := strings.Split(segment, "/")
		if len(parts) != 2 {
			return errors.New("invalid step syntax")
		}
		base = parts[0]
		parsedStep, err := strconv.Atoi(parts[1])
		if err != nil || parsedStep <= 0 {
			return errors.New("step must be a positive integer")
		}
		step = parsedStep
	}

	start := min
	end := max
	switch {
	case base == "*" || base == "":
	case strings.Contains(base, "-"):
		parts := strings.Split(base, "-")
		if len(parts) != 2 {
			return errors.New("invalid range syntax")
		}
		rangeStart, err := strconv.Atoi(parts[0])
		if err != nil {
			return errors.New("invalid range start")
		}
		rangeEnd, err := strconv.Atoi(parts[1])
		if err != nil {
			return errors.New("invalid range end")
		}
		start = rangeStart
		end = rangeEnd
	default:
		value, err := strconv.Atoi(base)
		if err != nil {
			return errors.New("invalid value")
		}
		start = value
		end = value
	}

	if start < min || end > max || start > end {
		return errors.New("segment value out of range")
	}

	for value := start; value <= end; value += step {
		values[value] = struct{}{}
	}
	return nil
}
