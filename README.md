#Fake-Nexus
Maven仓库库的代理/缓存程序
主要解决国内访问中央库缓慢的问题
适合小团队和个人开发者
引用于安全性要求不高的场合

- 可以在64M内存的VPS,例如搬瓦工,virmach等,无需nexus复杂的配置
- 支持简单上传功能,可以发布自己的jar.gradle已经测试过
- 支持gradle-warpper缓存

gradle配置
```groovy
    buildscript {
        repositories {
            maven {
                url "http://yourHost/maven/"
            }
        }
    }
    repositories {
        maven {
            url "http://yourHost/maven/"
        }
    }
```

gradle加速需要将在gradle-wrapper.properties替换
 ```
    distributionUrl=http://yourHost/gradle/gradle-2.xx-all.zip
 ```
