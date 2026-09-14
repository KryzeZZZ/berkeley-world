using UnityEngine;

namespace Sao.UnityClient
{
    public static class PixelWorldBootstrap
    {
        [RuntimeInitializeOnLoadMethod(RuntimeInitializeLoadType.AfterSceneLoad)]
        private static void CreateRuntime()
        {
            if (Object.FindObjectOfType<PixelWorldRuntime>() != null) return;
            var root = new GameObject("SAO Pixel World Runtime");
            root.AddComponent<PixelWorldApi>();
            root.AddComponent<PixelWorldRuntime>();
        }
    }
}
