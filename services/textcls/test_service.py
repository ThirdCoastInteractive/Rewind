from fastapi.testclient import TestClient
import app
from models import MODELS, ModelUnavailable, installed

client = TestClient(app.app)


def test_health_and_empty_batch():
    assert client.get("/health").json()["ok"] is True
    response = client.post("/v1/classify", json={"items": []})
    assert response.status_code == 200
    assert response.json() == {"items": []}


def test_models_list_without_weights():
    payload = client.get("/v1/models").json()
    assert set(m["name"] for m in payload["models"]) == set(MODELS)
    assert payload["max_batch"] == 64


def test_classify_waiting_model_without_weights():
    response = client.post(
        "/v1/classify",
        json={"items": [{"id": "1", "text": "hello"}]},
    )
    assert response.status_code == 503
    assert "waiting_model" in response.text


def test_unsupported_model_names_raise():
    try:
        installed("not-a-model")
        assert False, "accepted unknown model"
    except ModelUnavailable:
        pass
