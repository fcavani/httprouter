// Copyright 2010 Felipe Alves Cavani. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

// Start date:		2010-07-14
// Last modification:	2018-06

package httprouter

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"

	uni "github.com/fcavani/unicode"

	"github.com/fcavani/e"
)

// TypeParam representes one parameter and its Q value.
type TypeParam struct {
	Type string
	Q    int32 // X 1000 (times 1000)
}

func (tp *TypeParam) String() string {
	return fmt.Sprintf("%v; q=%v", tp.Type, (float64(tp.Q))/1000)
}

func (tp *TypeParam) GoString() string {
	Type := reflect.ValueOf(tp).Type()
	Type2 := reflect.ValueOf(tp.Type).Type()
	Type3 := reflect.ValueOf(tp.Q).Type()
	return fmt.Sprintf("%v{%v{%#v}, %v{%#v}}", Type, Type2, tp.Type, Type3, (float64(tp.Q))/1000)
}

// TypeParams is a collection of parameters.
type TypeParams []*TypeParam

func (tps TypeParams) Len() int {
	return len(tps)
}

// less determina the order of two parameter.
func less(l, r *TypeParam) bool {
	if l.Type == "*/*" || l.Type == "*" {
		return true
	}

	// Quality parameter is equal
	if l.Q == r.Q {
		if l.Type == r.Type {
			return false
		}

		si := strings.SplitN(l.Type, "/", 2)
		sj := strings.SplitN(r.Type, "/", 2)
		if len(si) == 2 && len(sj) == 2 {
			// Type is the same
			if si[0] == sj[0] {
				// Sub-type is generic
				if si[1] == "*" {
					return false
				}
				pi := strings.Split(si[1], ";")
				pj := strings.Split(sj[1], ";")
				// Smaller number of parameters loose.
				return len(pi) < len(pj)
			}
			return false
		} else {
			if len(l.Type) < len(r.Type) {
				return true
			}
		}
		return false
	}
	// Quality parameter is less
	return l.Q < r.Q
}

func (tps TypeParams) Less(i, j int) bool {
	return !less(tps[i], tps[j])
}

func (tps TypeParams) Swap(i, j int) {
	aux := tps[i]
	tps[i] = tps[j]
	tps[j] = aux
}

func (tps TypeParams) IsPresent(t string) bool {
	i := sort.Search(len(tps), func(i int) bool { return tps[i].Type == t })
	if i < len(tps) && tps[i].Type == t {
		return true
	}
	return false
}

const ErrInvalidCharInTypeParams = "character invalid in type param string"
const ErrTypeParamsLength = "invalid length"

func CheckTypeParams(s string, min, max int) error {
	if len(s) < min || len(s) > max {
		return e.New(ErrTypeParamsLength)
	}
	for _, v := range s {
		if !uni.IsLetter(v) && !unicode.IsDigit(v) && v != '-' && v != '/' && v != '=' && v != ';' && v != '.' {
			return e.Push(e.New(ErrInvalidCharInTypeParams), e.New("the character '%v' is invalid", string([]byte{byte(v)})))
		}
	}
	return nil
}

// Parse parses a string with a collection of type and parameters.
func Parse(s string) (TypeParams, error) {
	if len(s) == 0 {
		return nil, e.New("invalid type/parameter")
	}

	//"da, en-gb;q=0.8, en;q=0.7"
	mediaParams := strings.Split(s, ",")
	tps := make(TypeParams, len(mediaParams))
	count := 0
	for _, mp := range mediaParams {
		mps := strings.Split(mp, ";")
		if len(mps) <= 0 {
			return nil, e.New("invalid index")
		} else if len(mps) == 1 {
			tps[count] = &TypeParam{strings.TrimSpace(mps[0]), 1000}
			count = count + 1
			continue
		}
		var quality int32 = 1000
		strType := strings.TrimSpace(mps[0])
		for _, m := range mps[1:] {
			m = strings.TrimSpace(m)
			switch {
			case strings.HasPrefix(m, "q="):
				s := strings.SplitN(m, "=", 2)
				if s == nil {
					return nil, e.New("invalid quality value string")
				}
				if len(s) < 2 {
					return nil, e.New("small quality value string")
				}
				f, err := strconv.ParseFloat(s[1], 64)
				if err != nil {
					return nil, err
				}
				quality = int32(f * 1000.0)

			default:
				s := strings.SplitN(m, "=", 2)
				if s == nil {
					return nil, e.New("invalid token value string")
				}
				if len(s) < 2 {
					return nil, e.New("small token value string")
				}
				strType = strType + ";" + m
			}
		}
		tps[count] = &TypeParam{strType, quality}
		count = count + 1
	}
	tps = tps[0:count]
	if !sort.IsSorted(tps) {
		sort.Sort(tps)
	}
	return tps, nil
}

// ParseLang language making fall backs which the country part of the language
// definition when language have "-".
func ParseLang(s string) (TypeParams, error) {
	tps, err := Parse(s)
	if err != nil {
		return nil, e.Forward(err)
	}
	tpdmap := make(map[string]*TypeParam)
	for _, tp := range tps {
		s := strings.SplitN(tp.Type, "-", 2)
		if len(s) > 1 {
			if !tps.IsPresent(s[0]) {
				if x, ok := tpdmap[s[0]]; ok {
					if tp.Q > x.Q {
						x.Q = tp.Q
					}
				} else {
					tpdmap[s[0]] = &TypeParam{s[0], tp.Q}
				}
			}
		}
	}
	for _, tp := range tpdmap {
		tps = append(tps, tp)
	}
	if !sort.IsSorted(tps) {
		sort.Sort(tps)
	}
	return tps, nil
}

// comptypes compares two type, accept *.
func comptypes(l, r string) bool {
	if l == r {
		return true
	}

	ltp := strings.Split(strings.ToLower(l), ";")
	rtp := strings.Split(strings.ToLower(r), ";")
	if len(ltp) == 0 || len(rtp) == 0 {
		return false
	}

	ls := strings.SplitN(ltp[0], "/", 2)
	rs := strings.SplitN(rtp[0], "/", 2)
	//fmt.Println(ls, rs)

	switch {
	case len(ls) == 0 || len(rs) == 0:
		return false
	case len(ls) == 1 && ls[0] == "*" && len(rs) >= 1:
		//return true
	case len(ls) == 2 && len(rs) == 2 && ls[0] == "*" && ls[1] == "*":
		//return true
	case len(ls) == len(rs) && len(rs) == 2 && ls[0] == "*" && ls[1] != "*" && rs[0] != "" && rs[0] != " " && ls[1] == rs[1]:
		//return true
	case len(ls) == len(rs) && len(rs) == 2 && ls[0] != "*" && ls[1] == "*" && rs[1] != "" && rs[1] != " " && ls[0] == rs[0]:
		//return true
	default:
		return false
	}

	if len(ltp) < 2 && len(rtp) < 2 {
		return true
	}

	if len(ltp) != len(rtp) {
		return false
	}

	for i, l := range ltp[1:] {
		r := rtp[i+1]
		if r != l {
			return false
		}
	}

	return true
}

// rankType return the Q of the similar type t in tps.
func (tps TypeParams) rankType(t string) int32 {
	for _, tp := range tps {
		if comptypes(tp.Type, t) {
			return tp.Q
		}
	}
	return -1
}

// FindBest finds the more ranked type vs.
func (tps TypeParams) FindBest(vs map[string]struct{}) string {
	if len(vs) == 0 {
		return ""
	}
	best := &TypeParam{}
	for s, _ := range vs {
		s = strings.TrimSpace(s)
		tp := &TypeParam{s, tps.rankType(s)}
		if tp.Q > -1 {
			if less(best, tp) {
				best = tp
			}
		}
	}
	// If don't find anything pick the default one.
	//     if best.Type == "" {
	// 		return vs[0]
	// 	}
	return best.Type
}

func Subtype(typeparms string) (string, error) {
	s := strings.Split(typeparms, ";")
	if len(s) <= 0 {
		return "", e.New("invalid type/parameter")
	}
	t := strings.Split(s[0], "/")
	if len(t) == 2 {
		return t[1], nil
	}
	return "", e.New("subtype not found")
}
