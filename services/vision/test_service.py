from io import BytesIO
import json
import numpy as np
from PIL import Image
from fastapi.testclient import TestClient
import app
from inference import array_string

client = TestClient(app.app)

def test_contract_and_missing_models():
    assert client.get('/ping').text == 'pong'
    assert client.post('/predict', data={'entries': '{}'}).status_code == 422
    response = client.post('/predict', data={'entries': json.dumps({'clip': {'textual': {'modelName': 'unknown'}}}), 'text': 'test'})
    assert response.status_code == 503
    assert 'waiting_model' in response.text
    assert client.post('/v1/runtime/release').json() == {'released': True}

def test_string_vectors():
    assert len(json.loads(array_string(np.ones(512)))) == 512
    for vector in [np.zeros(512), np.ones(511), np.full(512, np.nan)]:
        try:
            array_string(vector)
            assert False, 'accepted invalid vector'
        except ValueError:
            pass

def test_batch_ids(monkeypatch):
    monkeypatch.setattr(app.runtime, 'predict', lambda **kwargs: {'clip': '[1,2]'})
    raw = BytesIO()
    Image.new('RGB', (10, 20)).save(raw, format='JPEG')
    response = client.post('/v1/predict-batch', data={'entries': '{}', 'ids': '["a","b"]'}, files=[('images', ('a.jpg', raw.getvalue())), ('images', ('b.jpg', raw.getvalue()))])
    assert [r['id'] for r in response.json()['results']] == ['a', 'b']


def test_face_tasks_and_models_are_removed():
    from models import MODELS, installed, ModelUnavailable
    assert set(MODELS) == {"ViT-B-32__openai"}
    try:
        installed("buffalo_l")
        assert False, "retired model was accepted"
    except ModelUnavailable:
        pass
    response = client.post('/predict', data={
        'entries': json.dumps({'facial-recognition': {'recognition': {'modelName': 'buffalo_l'}}}),
        'text': 'test'
    })
    assert response.status_code == 422
