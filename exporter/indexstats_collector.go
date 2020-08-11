// mnogo_exporter
// Copyright (C) 2017 Percona LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package exporter

import (
	"context"
	"fmt"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type indexstatsCollector struct {
	client      *mongo.Client
	collections []string
}

func (d *indexstatsCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(d, ch)
}

func (d *indexstatsCollector) Collect(ch chan<- prometheus.Metric) {
	// prometheus.Collector interface only receives a channel but no a context.
	// In order to make it possible to cancel, we should change the general implementation
	// and create a new collector instance every time we want to collect data and pass the
	// context as a parameter in a New() method. Until that's implemented, context = TODO()
	ctx := context.TODO()

	for _, dbCollection := range d.collections {
		parts := strings.Split(dbCollection, ".")
		if len(parts) != 2 { //nolint:gomnd
			continue
		}

		database := parts[0]
		collection := parts[1]

		aggregation := bson.D{
			{Key: "$indexStats", Value: bson.M{}},
		}

		cursor, err := d.client.Database(database).Collection(collection).Aggregate(ctx, mongo.Pipeline{aggregation})
		if err != nil {
			logrus.Errorf("cannot get $indexStats cursor for collection %s.%s: %s", database, collection, err)
			continue
		}

		var stats []bson.M
		if err = cursor.All(ctx, &stats); err != nil {
			logrus.Errorf("cannot get $indexStats for collection %s.%s: %s", database, collection, err)
			continue
		}

		logrus.Debugf("indexStats for %s.%s", database, collection)
		debugResult(stats)

		for _, m := range stats {
			// prefix and labels are needed to avoid duplicated metric names since the metrics are the
			// same, for different collections.
			prefix := fmt.Sprintf("%s_%s_%s", database, collection, m["name"])
			labels := map[string]string{
				"namespace": database + "." + collection,
				"key_name":  fmt.Sprintf("%s", m["name"]),
			}
			metrics := sanitizeMetrics(m)
			for _, metric := range makeMetrics(prefix, metrics, labels, false) {
				ch <- metric
			}
		}
	}
}

// According to specs, we should expose only this 2 metrics. 'building' might not exist.
func sanitizeMetrics(m bson.M) bson.M {
	ops := float64(0)

	if val := walkTo(m, []string{"accesses", "ops"}); val != nil {
		if f, err := asFloat64(val); err == nil {
			ops = *f
		}
	}

	filteredMetrics := bson.M{
		"accesses": bson.M{
			"ops": ops,
		},
	}

	if val := walkTo(m, []string{"building"}); val != nil {
		if f, err := asFloat64(val); err == nil {
			filteredMetrics["building"] = *f
		}
	}

	return filteredMetrics
}

var _ prometheus.Collector = (*indexstatsCollector)(nil)
