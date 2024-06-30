package main

import (
	"bufio"
	"context"
	"flag"
	clientv3 "go.etcd.io/etcd/client/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"
)

var merchants = []string{
	"e00", "e01", "e02", "e03", "e04", "e05", "e06", "e07", "e08", "e09", "e10", "e11", "e12", "e13", "e14",
	"e15", "e16", "e17", "e18", "e19", "e20", "e21", "e22", "e23", "e24", "e25", "e26", "e27", "e28", "e29",
	"e30", "e31", "e32", "e33", "e34", "e35", "e36", "e37", "e38", "e39", "e41", "e42", "e43", "e44", "e45",
	"e46", "e47", "e48", "e49", "e50", "e51", "e52", "e53", "e54", "e55", "e56", "e57", "e58", "e59", "e60",
	"e61", "e62", "e63", "e64", "e65", "e66", "e67", "e68", "e69", "e70", "e71", "e72", "e73", "e74", "e75",
	"e76", "e77", "e78", "e80", "e81", "e82", "e83", "e84", "e85", "e86", "e87", "e88", "e89", "e90", "e91",
	"e92", "e93", "e94", "e95", "e96", "e97", "e98", "e99",
}

func main() {
	kubeconfig := flag.String("kubeconfig", "/root/.kube/config", "absolute path to the kubeconfig file")
	flag.Parse()

	//创建 ETCD 客户端
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"http://52.67.125.34:2379"},
		DialTimeout: 10 * time.Second,
	})
	if err != nil {
		log.Fatalf("创建 ETCD 客户端失败: %v\n", err)
	}
	defer cli.Close()

	resp, err := cli.Get(context.Background(), "/", clientv3.WithPrefix())
	if err != nil {
		log.Fatalf("获取 .toml 文件列表失败: %vn", err)
	}

	var tomlFiles []string
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		// 提取文件名部分
		fileName := strings.TrimPrefix(key, ".toml/")
		tomlFiles = append(tomlFiles, fileName)
	}

	db := 2159

	for _, tomlFile := range tomlFiles {
		for _, merchantName := range merchants {
			if strings.Contains(tomlFile, merchantName) {
				log.Println("正在操作etcd：", tomlFile)
				resp, err := cli.Get(context.Background(), tomlFile)
				if err != nil {
					log.Fatalf("获取原始文件内容失败: %vn", err)
				}

				originFile := "./" + merchantName + "-origin" + ".toml"
				// 将内容保存到文件
				if err := os.WriteFile(originFile, resp.Kvs[0].Value, 0644); err != nil {
					log.Fatalf("保存文件失败: %v\n", err)
				}
				log.Printf("已将内容保存到文件: %s\n", originFile)

				// 开始修改内容
				getOriginContent, err := os.Open(originFile)
				if err != nil {
					log.Fatalf("无法打开文件: %v", err)
				}
				defer getOriginContent.Close()

				// 创建修改后的内容变量
				var modifiedContent []string
				inRedisSection := false
				// 逐行读取文件内容并修改 [redis] 部分
				scanner := bufio.NewScanner(getOriginContent)

				for scanner.Scan() {
					line := scanner.Text()

					// 判断是否进入了 [redis] 部分
					if strings.HasPrefix(line, "[redis]") {
						inRedisSection = true
						modifiedContent = append(modifiedContent, line)
						continue
					}

					// 如果已进入 [redis] 部分，则修改配置
					if inRedisSection {
						if strings.HasPrefix(line, "addr") {
							line = "addr = [\"172.31.49.200:26379\",\"172.31.49.202:26379\",\"172.31.49.203:26379\"]"
						} else if strings.HasPrefix(line, "password") {
							line = "password = \"h37F5Zt6Ckdh\""
						} else if strings.HasPrefix(line, "db") {
							line = "db = " + strconv.Itoa(db+1)
							db += 1
						} else if strings.HasPrefix(line, "sentinel") {
							line = "sentinel = \"br_redis\""
						}

						if line == "" {
							inRedisSection = false
							//continue
						}
					}

					// 添加修改后的行到修改后的内容中
					modifiedContent = append(modifiedContent, line)
				}

				// 创建新文件保存修改后的内容
				resFile := "./" + merchantName + "-res.toml"
				if err := os.WriteFile(resFile, []byte(strings.Join(modifiedContent, "\n")), 0644); err != nil {
					log.Fatalf("保存文件失败: %v\n", err)
				}
				log.Printf("已将修改后的内容保存到文件: %sn", resFile)

				// 上传到etcd
				resContent, err := os.ReadFile("./" + merchantName + "-res.toml")
				if err != nil {
					log.Fatalf("读取文件内容失败: %vn", err)
				}

				cli.Put(context.Background(), tomlFile, string(resContent))
				restartDeployment(merchantName, *kubeconfig)
			}

		}

	}
}

func restartDeployment(merchantName, kubeconfig string) {
	log.Println("当前操作的namespace: ", merchantName)

	// 创建 Kubernetes 客户端
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatalf("创建 Kubernetes 配置失败: %v", err)
	}
	clientside, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("创建 Kubernetes 客户端失败: %v", err)
	}

	// 存储需要重启的 Deployment 名称
	deploymentNames := []string{"bsigame", "bsimember", "bsimerchant", "bsitask", "bsigame-rpc"}

	// 遍历 Deployment 名称，并重启每个 Deployment
	for _, deploymentName := range deploymentNames {

		retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			log.Printf("正在操作：namespace: %v, Deployment: %v \n", merchantName, deploymentName)
			deployment, err := clientside.AppsV1().Deployments(merchantName).Get(context.TODO(), deploymentName, metav1.GetOptions{})
			if err != nil {
				log.Fatalf("获取 namespace: %v, Deployment: %v 失败: %v \n", merchantName, deploymentName, err)
			}

			// 确保 Annotations 不为 nil
			if deployment.Spec.Template.ObjectMeta.Annotations == nil {
				deployment.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
			}

			// 触发 Deployment 的滚动更新
			deployment.Spec.Template.ObjectMeta.Annotations["deployment.kubernetes.io/revision"] = generate2DigitString()
			_, updateErr := clientside.AppsV1().Deployments(merchantName).Update(context.TODO(), deployment, metav1.UpdateOptions{})
			return updateErr
		})
		if retryErr != nil {
			log.Fatalf("重启 namespace: %v Deployment: %v 失败: %vn", merchantName, deploymentName, retryErr)
		}

		log.Printf("已成功重启 Deployment: %s in Namespace: %s\n", deploymentName, merchantName)
		log.Println("sleep 0.8秒，防止EKS超时")
		time.Sleep(5000 * time.Millisecond)
	}

}
func generate2DigitString() string {
	num1 := rand.Intn(90) + 10 // 生成 10-99 之间的随机数
	num2 := rand.Intn(90) + 10
	return strconv.Itoa(num1) + strconv.Itoa(num2)
}
