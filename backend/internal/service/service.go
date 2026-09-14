// Package service 提供用例编排层：一个用户动作一个服务方法。
//
// 硬规则：事务/回滚边界在此；调用 repository / component / source，不碰 HTTP（docs/architecture.md §8.2）。
package service
