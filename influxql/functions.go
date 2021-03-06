package influxql

// All aggregate and query functions are defined in this file along with any intermediate data objects they need to process.
// Query functions are represented as two discreet functions: Map and Reduce. These roughly follow the MapReduce
// paradigm popularized by Google and Hadoop.
//
// When adding an aggregate function, define a mapper, a reducer, and add them in the switch statement in the MapReduceFuncs function

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
)

// Iterator represents a forward-only iterator over a set of points.
// These are used by the MapFunctions in this file
type Iterator interface {
	Next() (seriesID uint64, timestamp int64, value interface{})
}

// MapFunc represents a function used for mapping over a sequential series of data.
// The iterator represents a single group by interval
type MapFunc func(Iterator) interface{}

// ReduceFunc represents a function used for reducing mapper output.
type ReduceFunc func([]interface{}) interface{}

// UnmarshalFunc represents a function that can take bytes from a mapper from remote
// server and marshal it into an interface the reduer can use
type UnmarshalFunc func([]byte) (interface{}, error)

// InitializeMapFunc takes an aggregate call from the query and returns the MapFunc
func InitializeMapFunc(c *Call) (MapFunc, error) {
	// see if it's a query for raw data
	if c == nil {
		return MapRawQuery, nil
	}

	// Ensure that there is either a single argument or if for percentile, two
	if c.Name == "percentile" {
		if len(c.Args) != 2 {
			return nil, fmt.Errorf("expected two arguments for percentile()")
		}
	} else if len(c.Args) != 1 {
		return nil, fmt.Errorf("expected one argument for %s()", c.Name)
	}

	// Ensure the argument is a variable reference.
	_, ok := c.Args[0].(*VarRef)
	if !ok {
		return nil, fmt.Errorf("expected field argument in %s()", c.Name)
	}

	// Retrieve map function by name.
	switch c.Name {
	case "count":
		return MapCount, nil
	case "sum":
		return MapSum, nil
	case "mean":
		return MapMean, nil
	case "median":
		return MapStddev, nil
	case "min":
		return MapMin, nil
	case "max":
		return MapMax, nil
	case "spread":
		return MapSpread, nil
	case "stddev":
		return MapStddev, nil
	case "first":
		return MapFirst, nil
	case "last":
		return MapLast, nil
	case "percentile":
		_, ok := c.Args[1].(*NumberLiteral)
		if !ok {
			return nil, fmt.Errorf("expected float argument in percentile()")
		}
		return MapEcho, nil
	default:
		return nil, fmt.Errorf("function not found: %q", c.Name)
	}
}

// InitializeReduceFunc takes an aggregate call from the query and returns the ReduceFunc
func InitializeReduceFunc(c *Call) (ReduceFunc, error) {
	// Retrieve reduce function by name.
	switch c.Name {
	case "count":
		return ReduceSum, nil
	case "sum":
		return ReduceSum, nil
	case "mean":
		return ReduceMean, nil
	case "median":
		return ReduceMedian, nil
	case "min":
		return ReduceMin, nil
	case "max":
		return ReduceMax, nil
	case "spread":
		return ReduceSpread, nil
	case "stddev":
		return ReduceStddev, nil
	case "first":
		return ReduceFirst, nil
	case "last":
		return ReduceLast, nil
	case "percentile":
		if len(c.Args) != 2 {
			return nil, fmt.Errorf("expected float argument in percentile()")
		}

		lit, ok := c.Args[1].(*NumberLiteral)
		if !ok {
			return nil, fmt.Errorf("expected float argument in percentile()")
		}
		return ReducePercentile(lit.Val), nil
	default:
		return nil, fmt.Errorf("function not found: %q", c.Name)
	}
}

func InitializeUnmarshaller(c *Call) (UnmarshalFunc, error) {
	// if c is nil it's a raw data query
	if c == nil {
		return func(b []byte) (interface{}, error) {
			a := make([]*rawQueryMapOutput, 0)
			err := json.Unmarshal(b, &a)
			return a, err
		}, nil
	}

	// Retrieve marshal function by name
	switch c.Name {
	case "mean":
		return func(b []byte) (interface{}, error) {
			var o meanMapOutput
			err := json.Unmarshal(b, &o)
			return &o, err
		}, nil
	case "spread":
		return func(b []byte) (interface{}, error) {
			var o spreadMapOutput
			err := json.Unmarshal(b, &o)
			return &o, err
		}, nil
	case "first":
		return func(b []byte) (interface{}, error) {
			var o firstLastMapOutput
			err := json.Unmarshal(b, &o)
			return &o, err
		}, nil
	case "last":
		return func(b []byte) (interface{}, error) {
			var o firstLastMapOutput
			err := json.Unmarshal(b, &o)
			return &o, err
		}, nil
	case "stddev":
		return func(b []byte) (interface{}, error) {
			val := make([]float64, 0)
			err := json.Unmarshal(b, &val)
			return val, err
		}, nil
	case "median":
		return func(b []byte) (interface{}, error) {
			a := make([]float64, 0)
			err := json.Unmarshal(b, &a)
			return a, err
		}, nil
	default:
		return func(b []byte) (interface{}, error) {
			var val interface{}
			err := json.Unmarshal(b, &val)
			return val, err
		}, nil
	}
}

// MapCount computes the number of values in an iterator.
func MapCount(itr Iterator) interface{} {
	n := float64(0)
	for _, k, _ := itr.Next(); k != 0; _, k, _ = itr.Next() {
		n++
	}
	if n > 0 {
		return n
	}
	return nil
}

// MapSum computes the summation of values in an iterator.
func MapSum(itr Iterator) interface{} {
	n := float64(0)
	count := 0
	for _, k, v := itr.Next(); k != 0; _, k, v = itr.Next() {
		count++
		n += v.(float64)
	}
	if count > 0 {
		return n
	}
	return nil
}

// ReduceSum computes the sum of values for each key.
func ReduceSum(values []interface{}) interface{} {
	var n float64
	count := 0
	for _, v := range values {
		if v == nil {
			continue
		}
		count++
		n += v.(float64)
	}
	if count > 0 {
		return n
	}
	return nil
}

// MapMean computes the count and sum of values in an iterator to be combined by the reducer.
func MapMean(itr Iterator) interface{} {
	out := &meanMapOutput{}

	for _, k, v := itr.Next(); k != 0; _, k, v = itr.Next() {
		out.Count++
		out.Mean += (v.(float64) - out.Mean) / float64(out.Count)
	}

	if out.Count > 0 {
		return out
	}

	return nil
}

type meanMapOutput struct {
	Count int
	Mean  float64
}

// ReduceMean computes the mean of values for each key.
func ReduceMean(values []interface{}) interface{} {
	out := &meanMapOutput{}
	var countSum int
	for _, v := range values {
		if v == nil {
			continue
		}
		val := v.(*meanMapOutput)
		countSum = out.Count + val.Count
		out.Mean = val.Mean*(float64(val.Count)/float64(countSum)) + out.Mean*(float64(out.Count)/float64(countSum))
		out.Count = countSum
	}
	if out.Count > 0 {
		return out.Mean
	}
	return nil
}

// ReduceMedian computes the median of values
func ReduceMedian(values []interface{}) interface{} {
	var data []float64
	// Collect all the data points
	for _, value := range values {
		if value == nil {
			continue
		}
		data = append(data, value.([]float64)...)
	}

	length := len(data)
	if length < 2 {
		if length == 0 {
			return nil
		}
		return data[0]
	}
	middle := length / 2
	var sortedRange []float64
	if length%2 == 0 {
		sortedRange = getSortedRange(data, middle-1, 2)
		var low, high = sortedRange[0], sortedRange[1]
		return low + (high-low)/2
	} else {
		sortedRange = getSortedRange(data, middle, 1)
		return sortedRange[0]
	}
}

// getSortedRange returns a sorted subset of data. By using discardLowerRange and discardUpperRange to get the target
// subset (unsorted) and then just sorting that subset, the work can be reduced from O(N lg N), where N is len(data), to
// O(N + count lg count) for the average case
// - O(N) to discard the unwanted items
// - O(count lg count) to sort the count number of extracted items
// This can be useful for:
// - finding the median: getSortedRange(data, middle, 1)
// - finding the top N: getSortedRange(data, len(data) - N, N)
// - finding the bottom N: getSortedRange(data, 0, N)
func getSortedRange(data []float64, start int, count int) []float64 {
	out := discardLowerRange(data, start)
	k := len(out) - count
	if k > 0 {
		out = discardUpperRange(out, k)
	}
	sort.Float64s(out)

	return out
}

// discardLowerRange discards the lower k elements of the sorted data set without sorting all the data. Sorting all of
// the data would take O(NlgN), where N is len(data), but partitioning to find the kth largest number is O(N) in the
// average case. The remaining N-k unsorted elements are returned - no kind of ordering is guaranteed on these elements.
func discardLowerRange(data []float64, k int) []float64 {
	out := make([]float64, len(data)-k)
	i := 0

	// discard values lower than the desired range
	for k > 0 {
		lows, pivotValue, highs := partition(data)

		lowLength := len(lows)
		if lowLength > k {
			// keep all the highs and the pivot
			out[i] = pivotValue
			i++
			copy(out[i:], highs)
			i += len(highs)
			// iterate over the lows again
			data = lows
		} else {
			// discard all the lows
			data = highs
			k -= lowLength
			if k == 0 {
				// if discarded enough lows, keep the pivot
				out[i] = pivotValue
				i++
			} else {
				// able to discard the pivot too
				k--
			}
		}
	}
	copy(out[i:], data)
	return out
}

// discardUpperRange discards the upper k elements of the sorted data set without sorting all the data. Sorting all of
// the data would take O(NlgN), where N is len(data), but partitioning to find the kth largest number is O(N) in the
// average case. The remaining N-k unsorted elements are returned - no kind of ordering is guaranteed on these elements.
func discardUpperRange(data []float64, k int) []float64 {
	out := make([]float64, len(data)-k)
	i := 0

	// discard values higher than the desired range
	for k > 0 {
		lows, pivotValue, highs := partition(data)

		highLength := len(highs)
		if highLength > k {
			// keep all the lows and the pivot
			out[i] = pivotValue
			i++
			copy(out[i:], lows)
			i += len(lows)
			// iterate over the highs again
			data = highs
		} else {
			// discard all the highs
			data = lows
			k -= highLength
			if k == 0 {
				// if discarded enough highs, keep the pivot
				out[i] = pivotValue
				i++
			} else {
				// able to discard the pivot too
				k--
			}
		}
	}
	copy(out[i:], data)
	return out
}

// partition takes a list of data, chooses a random pivot index and returns a list of elements lower than the
// pivotValue, the pivotValue, and a list of elements higher than the pivotValue.  partition mutates data.
func partition(data []float64) (lows []float64, pivotValue float64, highs []float64) {
	length := len(data)
	// there are better (more complex) ways to calculate pivotIndex (e.g. median of 3, median of 3 medians) if this
	// proves to be inadequate.
	pivotIndex := rand.Int() % length
	pivotValue = data[pivotIndex]
	low, high := 1, length-1

	// put the pivot in the first position
	data[pivotIndex], data[0] = data[0], data[pivotIndex]

	// partition the data around the pivot
	for low <= high {
		for low <= high && data[low] <= pivotValue {
			low++
		}
		for high >= low && data[high] >= pivotValue {
			high--
		}
		if low < high {
			data[low], data[high] = data[high], data[low]
		}
	}

	return data[1:low], pivotValue, data[high+1:]
}

// MapMin collects the values to pass to the reducer
func MapMin(itr Iterator) interface{} {
	var min float64
	pointsYielded := false

	for _, k, v := itr.Next(); k != 0; _, k, v = itr.Next() {
		val := v.(float64)
		// Initialize min
		if !pointsYielded {
			min = val
			pointsYielded = true
		}
		min = math.Min(min, val)
	}
	if pointsYielded {
		return min
	}
	return nil
}

// ReduceMin computes the min of value.
func ReduceMin(values []interface{}) interface{} {
	var min float64
	pointsYielded := false

	for _, v := range values {
		if v == nil {
			continue
		}
		val := v.(float64)
		// Initialize min
		if !pointsYielded {
			min = val
			pointsYielded = true
		}
		m := math.Min(min, val)
		min = m
	}
	if pointsYielded {
		return min
	}
	return nil
}

// MapMax collects the values to pass to the reducer
func MapMax(itr Iterator) interface{} {
	var max float64
	pointsYielded := false

	for _, k, v := itr.Next(); k != 0; _, k, v = itr.Next() {
		val := v.(float64)
		// Initialize max
		if !pointsYielded {
			max = val
			pointsYielded = true
		}
		max = math.Max(max, val)
	}
	if pointsYielded {
		return max
	}
	return nil
}

// ReduceMax computes the max of value.
func ReduceMax(values []interface{}) interface{} {
	var max float64
	pointsYielded := false

	for _, v := range values {
		if v == nil {
			continue
		}
		val := v.(float64)
		// Initialize max
		if !pointsYielded {
			max = val
			pointsYielded = true
		}
		max = math.Max(max, val)
	}
	if pointsYielded {
		return max
	}
	return nil
}

type spreadMapOutput struct {
	Min, Max float64
}

// MapSpread collects the values to pass to the reducer
func MapSpread(itr Iterator) interface{} {
	var out spreadMapOutput
	pointsYielded := false

	for _, k, v := itr.Next(); k != 0; _, k, v = itr.Next() {
		val := v.(float64)
		// Initialize
		if !pointsYielded {
			out.Max = val
			out.Min = val
			pointsYielded = true
		}
		out.Max = math.Max(out.Max, val)
		out.Min = math.Min(out.Min, val)
	}
	if pointsYielded {
		return out
	}
	return nil
}

// ReduceSpread computes the spread of values.
func ReduceSpread(values []interface{}) interface{} {
	var result spreadMapOutput
	pointsYielded := false

	for _, v := range values {
		if v == nil {
			continue
		}
		val := v.(spreadMapOutput)
		// Initialize
		if !pointsYielded {
			result.Max = val.Max
			result.Min = val.Min
			pointsYielded = true
		}
		result.Max = math.Max(result.Max, val.Max)
		result.Min = math.Min(result.Min, val.Min)
	}
	if pointsYielded {
		return result.Max - result.Min
	}
	return nil
}

// MapStddev collects the values to pass to the reducer
func MapStddev(itr Iterator) interface{} {
	var values []float64

	for _, k, v := itr.Next(); k != 0; _, k, v = itr.Next() {
		values = append(values, v.(float64))
	}

	return values
}

// ReduceStddev computes the stddev of values.
func ReduceStddev(values []interface{}) interface{} {
	var data []float64
	// Collect all the data points
	for _, value := range values {
		if value == nil {
			continue
		}
		data = append(data, value.([]float64)...)
	}

	// If no data or we only have one point, it's nil or undefined
	if len(data) < 2 {
		return nil
	}

	// Get the mean
	var mean float64
	var count int
	for _, v := range data {
		count++
		mean += (v - mean) / float64(count)
	}
	// Get the variance
	var variance float64
	for _, v := range data {
		dif := v - mean
		sq := math.Pow(dif, 2)
		variance += sq
	}
	variance = variance / float64(count-1)
	stddev := math.Sqrt(variance)

	return stddev
}

type firstLastMapOutput struct {
	Time int64
	Val  interface{}
}

// MapFirst collects the values to pass to the reducer
func MapFirst(itr Iterator) interface{} {
	out := firstLastMapOutput{}
	pointsYielded := false

	for _, k, v := itr.Next(); k != 0; _, k, v = itr.Next() {
		// Initialize first
		if !pointsYielded {
			out.Time = k
			out.Val = v
			pointsYielded = true
		}
		if k < out.Time {
			out.Time = k
			out.Val = v
		}
	}
	if pointsYielded {
		return out
	}
	return nil
}

// ReduceFirst computes the first of value.
func ReduceFirst(values []interface{}) interface{} {
	out := firstLastMapOutput{}
	pointsYielded := false

	for _, v := range values {
		if v == nil {
			continue
		}
		val := v.(firstLastMapOutput)
		// Initialize first
		if !pointsYielded {
			out.Time = val.Time
			out.Val = val.Val
			pointsYielded = true
		}
		if val.Time < out.Time {
			out.Time = val.Time
			out.Val = val.Val
		}
	}
	if pointsYielded {
		return out.Val
	}
	return nil
}

// MapLast collects the values to pass to the reducer
func MapLast(itr Iterator) interface{} {
	out := firstLastMapOutput{}
	pointsYielded := false

	for _, k, v := itr.Next(); k != 0; _, k, v = itr.Next() {
		// Initialize last
		if !pointsYielded {
			out.Time = k
			out.Val = v
			pointsYielded = true
		}
		if k > out.Time {
			out.Time = k
			out.Val = v
		}
	}
	if pointsYielded {
		return out
	}
	return nil
}

// ReduceLast computes the last of value.
func ReduceLast(values []interface{}) interface{} {
	out := firstLastMapOutput{}
	pointsYielded := false

	for _, v := range values {
		if v == nil {
			continue
		}

		val := v.(firstLastMapOutput)
		// Initialize last
		if !pointsYielded {
			out.Time = val.Time
			out.Val = val.Val
			pointsYielded = true
		}
		if val.Time > out.Time {
			out.Time = val.Time
			out.Val = val.Val
		}
	}
	if pointsYielded {
		return out.Val
	}
	return nil
}

// MapEcho emits the data points for each group by interval
func MapEcho(itr Iterator) interface{} {
	var values []interface{}

	for _, k, v := itr.Next(); k != 0; _, k, v = itr.Next() {
		values = append(values, v)
	}
	return values
}

// ReducePercentile computes the percentile of values for each key.
func ReducePercentile(percentile float64) ReduceFunc {
	return func(values []interface{}) interface{} {
		var allValues []float64

		for _, v := range values {
			if v == nil {
				continue
			}

			vals := v.([]interface{})
			for _, v := range vals {
				allValues = append(allValues, v.(float64))
			}
		}

		sort.Float64s(allValues)
		length := len(allValues)
		index := int(math.Floor(float64(length)*percentile/100.0+0.5)) - 1

		if index < 0 || index >= len(allValues) {
			return nil
		}

		return allValues[index]
	}
}

// MapRawQuery is for queries without aggregates
func MapRawQuery(itr Iterator) interface{} {
	var values []*rawQueryMapOutput
	for _, k, v := itr.Next(); k != 0; _, k, v = itr.Next() {
		val := &rawQueryMapOutput{k, v}
		values = append(values, val)
	}
	return values
}

type rawQueryMapOutput struct {
	Timestamp int64
	Values    interface{}
}

type rawOutputs []*rawQueryMapOutput

func (a rawOutputs) Len() int           { return len(a) }
func (a rawOutputs) Less(i, j int) bool { return a[i].Timestamp < a[j].Timestamp }
func (a rawOutputs) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
