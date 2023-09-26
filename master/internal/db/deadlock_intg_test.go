//go:build integration
// +build integration

package db

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/shopspring/decimal"

	"github.com/determined-ai/determined/master/pkg/etc"
	"github.com/determined-ai/determined/master/pkg/model"
	"github.com/determined-ai/determined/master/pkg/ptrs"
)

func insertAllocation(t *testing.T, db *PgDB) model.AllocationID {
	// Add a mock user.
	user := RequireMockUser(t, db)
	// Add a job.
	jID := model.NewJobID()
	jIn := &model.Job{
		JobID:   jID,
		JobType: model.JobTypeExperiment,
		OwnerID: &user.ID,
		QPos:    decimal.New(0, 0),
	}
	err := db.AddJob(jIn)
	require.NoError(t, err, "failed to add job")
	tID := model.NewTaskID()
	tIn := &model.Task{
		TaskID:    tID,
		JobID:     &jID,
		TaskType:  model.TaskTypeTrial,
		StartTime: time.Now().UTC().Truncate(time.Millisecond),
	}
	err = db.AddTask(tIn)
	require.NoError(t, err, "failed to add task")
	aID := model.AllocationID(string(tID) + "-1")
	aIn := &model.Allocation{
		AllocationID: aID,
		TaskID:       tID,
		Slots:        8,
		ResourcePool: "somethingelse",
		StartTime:    ptrs.Ptr(time.Now().UTC().Truncate(time.Millisecond)),
	}
	err = db.AddAllocation(aIn)
	require.NoError(t, err)
	return aID
}

func TestTaskStatsDeadlock(t *testing.T) {
	require.NoError(t, etc.SetRootPath(RootFromDB))
	db := MustResolveTestPostgres(t)
	MustMigrateTestPostgres(t, db, MigrationsFromDB)
	aID := insertAllocation(t, db)
	aID2 := insertAllocation(t, db)
	// start := time.Now()
	var stats []*model.TaskStats
	for i := 0; i < 10000*10; i++ {
		start := time.Now()
		stats = append(stats, &model.TaskStats{
			AllocationID: aID,
			EventType:    "IMAGEPULL",
			StartTime:    &start,
		})
		time.Sleep(1 * time.Millisecond)
	}
	// for i := 0; i < 20; i++ {
	//     _, err := Bun().NewInsert().Model(&stats).Exec(context.TODO())
	//     require.NoError(t, err)
	// }
	_, err := Bun().NewInsert().Model(&stats).Exec(context.TODO())
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			id := aID
			if rand.Intn(2) == 1 {
				id = aID2
			}
			// time.Sleep(time.Millisecond * time.Duration(rand.Intn(10000)))
			t.Log("Recording", id)
			start := time.Now()
			err := db.RecordTaskStats(&model.TaskStats{
				AllocationID: id,
				EventType:    "IMAGEPULL",
				StartTime:    &start,
			})
			require.NoError(t, err)
			// time.Sleep(time.Millisecond * time.Duration(rand.Intn(10000)))
			t.Log("Recording end", id)
			end := time.Now()
			err = db.RecordTaskEndStatsBunTest(&model.TaskStats{
				AllocationID: id,
				EventType:    "IMAGEPULL",
				StartTime:    &start,
				EndTime:      &end,
			}, i)
			require.NoError(t, err)
		}()
	}
	wg.Wait()
}
