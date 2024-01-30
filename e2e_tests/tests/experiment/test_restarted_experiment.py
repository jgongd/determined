import pytest

from determined.common.api.bindings import experimentv1State
from tests import config as conf
from tests import experiment as exp
from tests import cluster

@pytest.mark.e2e_cpu
def test_experiment_capture() -> None:
    start_time = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    experiment_id = exp.create_experiment(
        conf.fixtures_path("no_op/single.yaml"), conf.fixtures_path("no_op")
    )
    exp.wait_for_experiment_state(experiment_id, experimentv1State.RUNNING)

    # allocation id
    tasks_data = cluster._task_list_json(conf.make_master_url())

    end_time = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    r = api.get(
        conf.make_master_url(),
        f"{API_URL}timestamp_after={start_time}&timestamp_before={end_time}",
    )
    assert r.status_code == requests.codes.ok, r.text

    # Check if an entry exists for experiment that just ran
    reader = csv.DictReader(StringIO(r.text))
    matches = [row for row in reader if int(row["experiment_id"]) == experiment_id]
    assert len(matches) >= 1, f"could not find any rows for experiment {experiment_id}"