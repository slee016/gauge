// Copyright 2015 ThoughtWorks, Inc.

// This file is part of Gauge.

// Gauge is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// Gauge is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.

// You should have received a copy of the GNU General Public License
// along with Gauge.  If not, see <http://www.gnu.org/licenses/>.

package filter

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/getgauge/gauge/gauge"
	"github.com/getgauge/gauge/logger"
	"golang.org/x/tools/go/exact"
	"golang.org/x/tools/go/types"
)

var currentTagExp string

type scenarioIndexFilterToRetain struct {
	indexToNotFilter     int
	currentScenarioIndex int
}
type ScenarioFilterBasedOnTags struct {
	specTags      []string
	tagExpression string
}

func newScenarioIndexFilterToRetain(index int) *scenarioIndexFilterToRetain {
	return &scenarioIndexFilterToRetain{index, 0}
}

func (filter *scenarioIndexFilterToRetain) Filter(item gauge.Item) bool {
	if item.Kind() == gauge.ScenarioKind {
		if filter.currentScenarioIndex != filter.indexToNotFilter {
			filter.currentScenarioIndex++
			return true
		} else {
			filter.currentScenarioIndex++
			return false
		}
	}
	return false
}

func newScenarioFilterBasedOnTags(specTags []string, tagExp string) *ScenarioFilterBasedOnTags {
	return &ScenarioFilterBasedOnTags{specTags, tagExp}
}

func (filter *ScenarioFilterBasedOnTags) Filter(item gauge.Item) bool {
	if item.Kind() == gauge.ScenarioKind {
		tags := item.(*gauge.Scenario).Tags
		if tags == nil {
			return !filter.filterTags(filter.specTags)
		}
		return !filter.filterTags(append(tags.Values, filter.specTags...))
	}
	return false
}

func (filter *ScenarioFilterBasedOnTags) filterTags(stags []string) bool {
	tagsMap := make(map[string]bool, 0)
	for _, tag := range stags {
		tagsMap[strings.Replace(tag, " ", "", -1)] = true
	}
	filter.replaceSpecialChar()
	value, _ := filter.formatAndEvaluateExpression(tagsMap, filter.isTagPresent)
	return value
}

func (filter *ScenarioFilterBasedOnTags) replaceSpecialChar() {
	filter.tagExpression = strings.Replace(strings.Replace(strings.Replace(strings.Replace(filter.tagExpression, " ", "", -1), ",", "&", -1), "&&", "&", -1), "||", "|", -1)
}

func (filter *ScenarioFilterBasedOnTags) formatAndEvaluateExpression(tagsMap map[string]bool, isTagQualified func(tagsMap map[string]bool, tagName string) bool) (bool, error) {
	_, tags := filter.getOperatorsAndOperands()
	expToBeEvaluated := filter.tagExpression
	sort.Sort(ByLength(tags))
	for _, tag := range tags {
		expToBeEvaluated = strings.Replace(expToBeEvaluated, strings.TrimSpace(tag), strconv.FormatBool(isTagQualified(tagsMap, strings.TrimSpace(tag))), -1)
	}
	return filter.evaluateExp(filter.handleNegation(expToBeEvaluated))
}

type ByLength []string

func (s ByLength) Len() int {
	return len(s)
}
func (s ByLength) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s ByLength) Less(i, j int) bool {
	return len(s[i]) > len(s[j])
}

func (filter *ScenarioFilterBasedOnTags) handleNegation(tagExpression string) string {
	tagExpression = strings.Replace(strings.Replace(tagExpression, "!true", "false", -1), "!false", "true", -1)
	for strings.Contains(tagExpression, "!(") {
		tagExpression = filter.evaluateBrackets(tagExpression)
	}
	return tagExpression
}

func (filter *ScenarioFilterBasedOnTags) evaluateBrackets(tagExpression string) string {
	if strings.Contains(tagExpression, "!(") {
		innerText := filter.resolveBracketExpression(tagExpression)
		return strings.Replace(tagExpression, "!("+innerText+")", filter.evaluateBrackets(innerText), -1)
	}
	value, _ := filter.evaluateExp(tagExpression)
	return strconv.FormatBool(!value)
}

func (filter *ScenarioFilterBasedOnTags) resolveBracketExpression(tagExpression string) string {
	indexOfOpenBracket := strings.Index(tagExpression, "!(") + 1
	bracketStack := make([]string, 0)
	i := indexOfOpenBracket
	for ; i < len(tagExpression); i++ {
		if tagExpression[i] == '(' {
			bracketStack = append(bracketStack, "(")
		} else if tagExpression[i] == ')' {
			bracketStack = append(bracketStack[:len(bracketStack)-1])
		}
		if len(bracketStack) == 0 {
			break
		}
	}
	return tagExpression[indexOfOpenBracket+1 : i]
}

func (filter *ScenarioFilterBasedOnTags) evaluateExp(tagExpression string) (bool, error) {
	tre := regexp.MustCompile("true")
	fre := regexp.MustCompile("false")

	s := fre.ReplaceAllString(tre.ReplaceAllString(tagExpression, "1"), "0")

	val, err := types.Eval(s, nil, nil)
	if err != nil {
		return false, errors.New("Invalid Expression.\n" + err.Error())
	}
	res, _ := exact.Uint64Val(val.Value)

	var final bool
	if res == 1 {
		final = true
	} else {
		final = false
	}

	return final, nil
}

func (filter *ScenarioFilterBasedOnTags) isTagPresent(tagsMap map[string]bool, tagName string) bool {
	_, ok := tagsMap[tagName]
	return ok
}

func (filter *ScenarioFilterBasedOnTags) getOperatorsAndOperands() ([]string, []string) {
	listOfOperators := make([]string, 0)
	listOfTags := strings.FieldsFunc(filter.tagExpression, func(r rune) bool {
		isValidOperator := r == '&' || r == '|' || r == '(' || r == ')' || r == '!'
		if isValidOperator {
			operator, _ := strconv.Unquote(strconv.QuoteRuneToASCII(r))
			listOfOperators = append(listOfOperators, operator)
			return isValidOperator
		}
		return false
	})
	return listOfOperators, listOfTags
}

func filterSpecsItems(specs []*gauge.Specification, filter gauge.SpecItemFilter) []*gauge.Specification {
	filteredSpecs := make([]*gauge.Specification, 0)
	for _, spec := range specs {
		spec.Filter(filter)
		if len(spec.Scenarios) != 0 {
			filteredSpecs = append(filteredSpecs, spec)
		}
	}
	return filteredSpecs
}

func filterSpecsByTags(specs []*gauge.Specification, tagExpression string) []*gauge.Specification {
	filteredSpecs := make([]*gauge.Specification, 0)
	for _, spec := range specs {
		tagValues := make([]string, 0)
		if spec.Tags != nil {
			tagValues = spec.Tags.Values
		}
		spec.Filter(newScenarioFilterBasedOnTags(tagValues, tagExpression))
		if len(spec.Scenarios) != 0 {
			filteredSpecs = append(filteredSpecs, spec)
		}
	}
	return filteredSpecs
}

func validateTagExpression(tagExpression string) {
	filter := &ScenarioFilterBasedOnTags{tagExpression: tagExpression}
	filter.replaceSpecialChar()
	_, err := filter.formatAndEvaluateExpression(make(map[string]bool, 0), func(a map[string]bool, b string) bool { return true })
	if err != nil {
		logger.Fatalf(err.Error())
	}
}
