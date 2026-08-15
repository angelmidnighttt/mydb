# Tài liệu mydb

Đọc theo thứ tự số. Mỗi file là một phần đã làm xong, kèm lý do vì sao làm như vậy.

| # | Tài liệu | Nội dung |
|---|---|---|
| 01 | [Tổng quan](01-tong-quan.md) | mydb là gì, cấu trúc thư mục, cách chạy, lộ trình |
| 02 | [In-memory store](02-in-memory-store.md) | `internal/store` — KV trong RAM, API và các quyết định thiết kế |
| 03 | [Serialization](03-serialization.md) | `internal/wal` — mã hóa entry thành byte, `io.Reader`/`io.Writer` |

## Quy ước

- Mỗi file mở đầu bằng **mục tiêu** của phần đó, kết thúc bằng **giới hạn hiện tại**.
  Phần giới hạn quan trọng không kém phần còn lại: nó là danh sách việc phải làm tiếp,
  và là lời nhắc rằng những gì chưa làm là *chưa làm*, không phải đã ổn.
- Code trong `docs/` chỉ để minh họa. Ví dụ chạy được nằm ở các file `example_test.go`
  trong từng package — chúng được `go test` kiểm tra nên không thể lỗi thời mà không ai biết.
