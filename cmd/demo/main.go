package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"SAO/internal/ai"
	"SAO/internal/config"
	"SAO/internal/model"
	"SAO/internal/scene"
	"SAO/internal/world"
)

func main() {
	_ = config.LoadEnvFile(envFilePath())

	worldManager := world.NewManager(nil)
	worldManager.EnablePersistence("data/world.json")

	scenePolicy, err := config.LoadScenePolicy("config/scene_policy.json")
	if err != nil {
		fmt.Printf("加载场景策略失败: %v\n", err)
		return
	}
	worldManager.SetAllowSceneCreation(scenePolicy.AllowSceneCreation)

	loaded, err := worldManager.LoadFromPersistence()
	if err != nil {
		fmt.Printf("从持久化加载失败: %v\n", err)
		return
	}
	if !loaded {
		sceneInstance := scene.NewSceneInstance("scene-起始城镇", 128, nil)
		sceneInstance.RegisterLayer("scene-地牢")
		worldManager.AddScene(sceneInstance)

		if err := worldManager.AddPlayer("scene-起始城镇", &model.PlayerState{ID: "p1", Name: "爱丽丝"}); err != nil {
			fmt.Printf("初始化失败: %v\n", err)
			return
		}
		if err := worldManager.AddObject("scene-起始城镇", &model.GameObject{
			ID:      "obj-哥布林",
			Name:    "哥布林",
			Tags:    []string{"非玩家角色", "敌对", "哥布林"},
			State:   map[string]any{"hp": 10, "layer": "scene-起始城镇"},
			Version: 1,
		}); err != nil {
			fmt.Printf("初始化失败: %v\n", err)
			return
		}
		if err := worldManager.AddObject("scene-起始城镇", &model.GameObject{
			ID:      "obj-木箱-1",
			Name:    "木箱",
			Tags:    []string{"容器", "木箱", "箱子"},
			State:   map[string]any{"locked": false, "opened": false, "layer": "scene-起始城镇"},
			Version: 1,
		}); err != nil {
			fmt.Printf("初始化失败: %v\n", err)
			return
		}
	}

	configPath := os.Getenv("AI_REFEREE_CONFIG")
	if strings.TrimSpace(configPath) == "" {
		configPath = "config/ai_referee.json"
	}
	referee, mode, err := ai.NewOptionalRefereeFromConfigFile(configPath)
	if err != nil {
		fmt.Printf("加载 AI 裁判失败: %v\n", err)
		return
	}

	fmt.Println("TRPG 交互演示")
	fmt.Println("玩家: p1, 输入自然语言指令，例如: 观察木箱、打开木箱")
	fmt.Printf("AI 模式: %s\n", mode)
	fmt.Printf("AI 配置: %s\n", configPath)
	fmt.Println("层级指令: /push <layer>, /pop, /replace <layer>, /addlayer <layer> [relation] [parent], /where, /visible, /inventory")
	fmt.Println("交互指令: /interact <target>, /observe <target>, /pick <target>, /drop <target>")
	fmt.Println("输入 exit 退出")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if strings.EqualFold(input, "exit") {
			_ = worldManager.FlushPersistence()
			break
		}
		if strings.HasPrefix(input, "/") {
			runLayerCommand(worldManager, "p1", input)
			continue
		}
		runDemoTurn(worldManager, referee, "p1", input)
	}
}

func envFilePath() string {
	if v := strings.TrimSpace(os.Getenv("ENV_FILE")); v != "" {
		return v
	}
	return ".env"
}

func runDemoTurn(worldManager *world.Manager, referee ai.Referee, playerID, input string) {
	turnStart := time.Now()

	step0Start := time.Now()
	parsed, err := referee.Call(input)
	step0Elapsed := time.Since(step0Start).Milliseconds()
	fmt.Printf("解析耗时: %dms\n", step0Elapsed)
	if err != nil {
		fmt.Printf("  解析失败: %v\n", err)
		return
	}
	printJSON("  解析结果", parsed)

	action := ai.ToModelAction(parsed)
	sceneInstance, err := worldManager.GetSceneByPlayer(playerID)
	if err != nil {
		fmt.Printf("获取玩家场景失败: %v\n", err)
		return
	}
	step1Start := time.Now()
	if action.Type == "interact" && action.TargetObjectID == "" && action.TargetQuery != "" {
		targetID, err := worldManager.ResolveTargetObjectID(playerID, action.TargetQuery)
		step1Elapsed := time.Since(step1Start).Milliseconds()
		fmt.Printf("解析目标耗时: %dms\n", step1Elapsed)
		if err != nil {
			fmt.Printf("  解析失败: %v\n", err)
			return
		}
		action.TargetObjectID = targetID
		fmt.Printf("  目标解析: target_query=%q -> target_object_id=%q\n", action.TargetQuery, targetID)
	} else if action.Type == "observe" && action.TargetObjectID == "" && action.TargetQuery != "" {
		targetID, err := worldManager.ResolveTargetObjectID(playerID, action.TargetQuery)
		if err == nil {
			step1Elapsed := time.Since(step1Start).Milliseconds()
			fmt.Printf("解析目标耗时: %dms\n", step1Elapsed)
			action.TargetObjectID = targetID
			fmt.Printf("  目标解析: target_query=%q -> target_object_id=%q\n", action.TargetQuery, targetID)
		} else {
			layerID, layerErr := sceneInstance.ResolveLayerID(playerID, action.TargetQuery)
			step1Elapsed := time.Since(step1Start).Milliseconds()
			fmt.Printf("解析目标耗时: %dms\n", step1Elapsed)
			if layerErr != nil {
				fmt.Printf("  解析失败: %v\n", err)
				return
			}
			action.LayerID = layerID
			fmt.Printf("  目标解析: target_query=%q -> layer_id=%q\n", action.TargetQuery, layerID)
		}
	} else {
		step1Elapsed := time.Since(step1Start).Milliseconds()
		fmt.Printf("解析目标耗时: %dms\n", step1Elapsed)
		fmt.Println("  无目标解析: 仅支持 interact/observe target_query")
	}
	pipelineStart := time.Now()
	trace := sceneInstance.ExecuteActionWithTrace(model.QueuedAction{PlayerID: playerID, Action: action})
	pipelineElapsed := time.Since(pipelineStart).Milliseconds()

	fmt.Printf("执行耗时: %dms\n", pipelineElapsed)
	for _, step := range trace.Steps {
		duration := extractDurationMS(step.Detail)
		if step.Success {
			fmt.Printf("  %s: 成功(%dms)\n", step.Name, duration)
			if len(step.Detail) > 0 {
				printJSON("    详情", step.Detail)
			}
		} else {
			fmt.Printf("  %s: 失败(%dms): %s\n", step.Name, duration, step.Error)
		}
	}
	fmt.Printf("本回合总耗时: %dms\n", time.Since(turnStart).Milliseconds())
	_ = worldManager.FlushPersistence()
}

func runLayerCommand(worldManager *world.Manager, playerID, input string) {
	sceneInstance, err := worldManager.GetSceneByPlayer(playerID)
	if err != nil {
		fmt.Printf("获取玩家场景失败: %v\n", err)
		return
	}
	parts := strings.Fields(strings.TrimSpace(input))
	if len(parts) == 0 {
		return
	}
	switch strings.ToLower(parts[0]) {
	case "/push":
		if len(parts) < 2 {
			fmt.Println("用法: /push <layer>")
			return
		}
		if err := sceneInstance.PushLayerForPlayer(playerID, parts[1]); err != nil {
			fmt.Printf("push 失败: %v\n", err)
			return
		}
		fmt.Printf("push 成功: %s\n", parts[1])
		printPlayerLayers(sceneInstance, playerID)
		_ = worldManager.FlushPersistence()
	case "/pop":
		popped, err := sceneInstance.PopLayerForPlayer(playerID)
		if err != nil {
			fmt.Printf("pop 失败: %v\n", err)
			return
		}
		fmt.Printf("pop 成功: %s\n", popped)
		printPlayerLayers(sceneInstance, playerID)
		_ = worldManager.FlushPersistence()
	case "/replace":
		if len(parts) < 2 {
			fmt.Println("用法: /replace <layer>")
			return
		}
		if err := sceneInstance.ReplaceLayerForPlayer(playerID, parts[1]); err != nil {
			fmt.Printf("replace 失败: %v\n", err)
			return
		}
		fmt.Printf("replace 成功: %s\n", parts[1])
		printPlayerLayers(sceneInstance, playerID)
		_ = worldManager.FlushPersistence()
	case "/where":
		printPlayerLayers(sceneInstance, playerID)
	case "/addlayer":
		if len(parts) < 2 {
			fmt.Println("用法: /addlayer <layer> [relation] [parent]")
			return
		}
		relation := "child"
		if len(parts) >= 3 {
			relation = parts[2]
		}
		parent := ""
		if len(parts) >= 4 {
			parent = parts[3]
		}
		createdID, err := worldManager.CreateLayerForPlayer(playerID, parts[1], relation, parent)
		if err != nil {
			fmt.Printf("addlayer 失败: %v\n", err)
			return
		}
		fmt.Printf("addlayer 成功: %s (relation=%s)\n", createdID, relation)
		_ = worldManager.FlushPersistence()
	case "/visible":
		objects := sceneInstance.GetVisibleObjects(playerID)
		names := make([]string, 0, len(objects))
		for _, obj := range objects {
			layer, _ := obj.State["layer"].(string)
			names = append(names, fmt.Sprintf("%s(id=%s,layer=%s)", obj.Name, obj.ID, layer))
		}
		sort.Strings(names)
		fmt.Println("可见物体:")
		for _, name := range names {
			fmt.Printf("  - %s\n", name)
		}
	case "/inventory":
		items, err := sceneInstance.GetPlayerInventory(playerID)
		if err != nil {
			fmt.Printf("获取背包失败: %v\n", err)
			return
		}
		fmt.Println("背包物品:")
		for _, item := range items {
			fmt.Printf("  - %s(id=%s)\n", item.Name, item.ID)
		}
	case "/interact":
		if len(parts) < 2 {
			fmt.Println("用法: /interact <target>")
			return
		}
		executeManualAction(worldManager, sceneInstance, playerID, model.Action{
			Type:        "interact",
			Interaction: "",
			TargetQuery: strings.TrimSpace(strings.Join(parts[1:], " ")),
			Payload:     map[string]any{"raw_input": input},
		})
	case "/observe":
		if len(parts) < 2 {
			fmt.Println("用法: /observe <target>")
			return
		}
		executeManualAction(worldManager, sceneInstance, playerID, model.Action{
			Type:        "observe",
			TargetQuery: strings.TrimSpace(strings.Join(parts[1:], " ")),
			Payload:     map[string]any{"raw_input": input},
		})
	case "/pick":
		if len(parts) < 2 {
			fmt.Println("用法: /pick <target>")
			return
		}
		executeManualAction(worldManager, sceneInstance, playerID, model.Action{
			Type:        "interact",
			Interaction: "pickup_item",
			TargetQuery: strings.TrimSpace(strings.Join(parts[1:], " ")),
			Payload:     map[string]any{"raw_input": input},
		})
	case "/drop":
		if len(parts) < 2 {
			fmt.Println("用法: /drop <target>")
			return
		}
		executeManualAction(worldManager, sceneInstance, playerID, model.Action{
			Type:        "interact",
			Interaction: "drop_item",
			TargetQuery: strings.TrimSpace(strings.Join(parts[1:], " ")),
			Payload:     map[string]any{"raw_input": input},
		})
	default:
		fmt.Println("未知指令")
	}
}

func executeManualAction(worldManager *world.Manager, sceneInstance *scene.SceneInstance, playerID string, action model.Action) {
	if action.TargetObjectID == "" && action.TargetQuery != "" {
		if action.Type == "observe" {
			if targetID, err := worldManager.ResolveTargetObjectID(playerID, action.TargetQuery); err == nil {
				action.TargetObjectID = targetID
			} else if layerID, layerErr := sceneInstance.ResolveLayerID(playerID, action.TargetQuery); layerErr == nil {
				action.LayerID = layerID
			} else {
				fmt.Printf("解析失败: %v\n", err)
				return
			}
		} else {
			targetID, err := worldManager.ResolveTargetObjectID(playerID, action.TargetQuery)
			if err != nil {
				fmt.Printf("解析失败: %v\n", err)
				return
			}
			action.TargetObjectID = targetID
		}
	}
	printJSON("manual action", action)
	trace := sceneInstance.ExecuteActionWithTrace(model.QueuedAction{PlayerID: playerID, Action: action})
	for _, step := range trace.Steps {
		duration := extractDurationMS(step.Detail)
		if step.Success {
			fmt.Printf("  %s: 成功(%dms)\n", step.Name, duration)
			if len(step.Detail) > 0 {
				printJSON("    详情", step.Detail)
			}
		} else {
			fmt.Printf("  %s: 失败(%dms): %s\n", step.Name, duration, step.Error)
		}
	}
	_ = worldManager.FlushPersistence()
}

func printPlayerLayers(sceneInstance *scene.SceneInstance, playerID string) {
	stack, err := sceneInstance.GetPlayerLayerStack(playerID)
	if err != nil {
		fmt.Printf("获取层级失败: %v\n", err)
		return
	}
	fmt.Printf("层级栈: %v\n", stack)
}

func printJSON(prefix string, value any) {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Printf("%s: <marshal error: %v>\n", prefix, err)
		return
	}
	fmt.Printf("%s:\n%s\n", prefix, string(b))
}

func extractDurationMS(detail map[string]any) int64 {
	if detail == nil {
		return 0
	}
	switch v := detail["duration_ms"].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}
