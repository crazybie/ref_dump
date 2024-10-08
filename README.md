# 内存泄露辅助检查工具

## 简述
帮助分析大对象因为被哪些为止对象引用，从而导致不能被gc释放内存，造成的内存泄露。

## 问题
地图对象挂载了非常多的子对象，内存开销巨大，副本开房间时会不断创建和销毁地推对象。因为某些代码不规范问题导致地图对象被非常隐蔽的对象引用，导致退出房间时，虽然执行了地图销毁流程，但是对象始终无法被系统gc回收，造成系统内存不足无法服务，只有重启强制释放。
希望通过工具找到隐蔽的引用地图对象的地方，然后销毁地图时断开引用，让系统正常回收。

## 分析
因为泄露的地图对象是已知的，如果能够反向找到所有引用它的父对象，再反向找到引用这些父对象的父对象，就可以找到泄露的源头。

## 思路
1. hook所有的内存调用，记录分配内存
2. 给定一个需要查找的引用地址，找到对应的内存块，遍历其他内存块，找出所有的父节点
3. 循环2，直到没有父节点
4. 输出所有的父节点路径

## 细节
1. 如何遍历程序的全局内存
2. 如何处理data race
3. 如何从内存块中识别出指针  
5. 如何hook运行时

## 其他实现
1. https://github.com/cloudwego/goref
