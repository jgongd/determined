package db

import (
	"context"

	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/uptrace/bun"

	"github.com/determined-ai/determined/master/pkg/model"
)

// AddJob persists the existence of a job.
func (db *PgDB) AddJob(j *model.Job) error {
	return AddJobTx(context.TODO(), Bun(), j)
}

// JobByID retrieves a job by ID.
func (db *PgDB) JobByID(jID model.JobID) (*model.Job, error) {
	var j model.Job
	if err := db.query(`
SELECT *
FROM jobs
WHERE job_id = $1
`, &j, jID); err != nil {
		return nil, errors.Wrap(err, "querying job")
	}
	return &j, nil
}

// AddJobTx persists the existence of a job from a tx.
func AddJobTx(ctx context.Context, idb bun.IDB, j *model.Job) error {
	_, err := idb.NewInsert().Model(j).Exec(ctx)
	if err != nil {
		return errors.Wrap(err, "adding job")
	}
	return nil
}

// UpdateJobPosition propagates the new queue position to the job.
func (db *PgDB) UpdateJobPosition(jobID model.JobID, position decimal.Decimal) error {
	if jobID.String() == "" {
		return errors.Errorf("error modifying job with empty id")
	}
	_, err := db.sql.Exec(`
UPDATE jobs
SET q_position = $2
WHERE job_id = $1`, jobID, position)
	return err
}
