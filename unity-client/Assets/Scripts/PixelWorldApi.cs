using System;
using System.Collections;
using System.Text;
using Newtonsoft.Json;
using UnityEngine;
using UnityEngine.Networking;

namespace Sao.UnityClient
{
    public sealed class PixelWorldApi : MonoBehaviour
    {
        [SerializeField] private string baseUrl = "http://127.0.0.1:8080";
        [SerializeField] private string playerId = "p1";

        public void GetWorld(Action<PixelWorldSnapshot> succeeded, Action<string> failed)
        {
            StartCoroutine(Send("GET", "/pixel/world", null, response =>
            {
                var envelope = JsonConvert.DeserializeObject<PixelWorldEnvelope>(response);
                succeeded?.Invoke(envelope.world);
            }, failed));
        }

        public void Move(GridPosition destination, Action<PixelWorldSnapshot> succeeded, Action<string> failed)
        {
            SendAction("/pixel/move", new { player_id = playerId, to = destination }, succeeded, failed);
        }

        public void Interact(string entityId, Action<PixelWorldSnapshot> succeeded, Action<string> failed)
        {
            SendAction("/pixel/interact", new { player_id = playerId, entity_id = entityId }, succeeded, failed);
        }

        public void UseItem(string itemId, GridPosition target, Action<PixelWorldSnapshot> succeeded, Action<string> failed)
        {
            SendAction("/pixel/use-item", new { player_id = playerId, item_id = itemId, target }, succeeded, failed);
        }

        public void GetMapTexture(int revision, Action<Texture2D> succeeded, Action<string> failed)
        {
            StartCoroutine(FetchMap(revision, succeeded, failed));
        }

        public void GetObjectTexture(string entityId, Action<Texture2D, bool> succeeded, Action<string> failed)
        {
            StartCoroutine(FetchObject(entityId, succeeded, failed));
        }

        private void SendAction(string path, object body, Action<PixelWorldSnapshot> succeeded, Action<string> failed)
        {
            SendAction(path, JsonConvert.SerializeObject(body), succeeded, failed);
        }

        private void SendAction(string path, string body, Action<PixelWorldSnapshot> succeeded, Action<string> failed)
        {
            StartCoroutine(Send("POST", path, body, response =>
            {
                var envelope = JsonConvert.DeserializeObject<PixelActionEnvelope>(response);
                succeeded?.Invoke(envelope.world);
            }, failed));
        }

        private IEnumerator Send(string method, string path, string body, Action<string> succeeded, Action<string> failed)
        {
            using (var request = new UnityWebRequest(baseUrl.TrimEnd('/') + path, method))
            {
                request.downloadHandler = new DownloadHandlerBuffer();
                if (body != null)
                {
                    request.uploadHandler = new UploadHandlerRaw(Encoding.UTF8.GetBytes(body));
                    request.SetRequestHeader("Content-Type", "application/json");
                }
                yield return request.SendWebRequest();
                if (request.result != UnityWebRequest.Result.Success)
                {
                    failed?.Invoke(request.downloadHandler.text.Length > 0 ? request.downloadHandler.text : request.error);
                    yield break;
                }
                succeeded?.Invoke(request.downloadHandler.text);
            }
        }

        private IEnumerator FetchMap(int revision, Action<Texture2D> succeeded, Action<string> failed)
        {
            using (var request = UnityWebRequestTexture.GetTexture(baseUrl.TrimEnd('/') + "/pixel/render/map.png?revision=" + revision, true))
            {
                yield return request.SendWebRequest();
                if (request.result != UnityWebRequest.Result.Success)
                {
                    failed?.Invoke(request.error);
                    yield break;
                }
                succeeded?.Invoke(DownloadHandlerTexture.GetContent(request));
            }
        }

        private IEnumerator FetchObject(string entityId, Action<Texture2D, bool> succeeded, Action<string> failed)
        {
            var url = baseUrl.TrimEnd('/') + "/pixel/render/object/" + UnityWebRequest.EscapeURL(entityId) + ".png";
            using (var request = UnityWebRequestTexture.GetTexture(url, true))
            {
                yield return request.SendWebRequest();
                if (request.result != UnityWebRequest.Result.Success)
                {
                    failed?.Invoke(request.downloadHandler.text.Length > 0 ? request.downloadHandler.text : request.error);
                    yield break;
                }
                var generating = request.GetResponseHeader("X-Pixel-Asset-Status") == "generating";
                succeeded?.Invoke(DownloadHandlerTexture.GetContent(request), generating);
            }
        }
    }
}
