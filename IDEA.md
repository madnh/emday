# emday

Tôi có nhiều server, nhiều khi các server bị thay đổi IP hoặc không có internet, bị treo, các vấn đề resources (CPU,RAM, ổ cứng,...) thì mất kết nối, phải tìm cách tìm kiếm thông tin lại, hoặc không chủ động xử lý trước khi bị xự cố được. Tôi muốn có 1 cơ chế thế này:
- 1 service chạy ngầm trên máy, tự động theo dõi các thông tin và gửi thông báo khi có thay đổi, các "thông tin" này dạng có thể tuỳ chỉnh hoặc các dịch vụ khác gửi thông tin vào service của tôi
- gửi thông báo khi điều gì đó thay đổi, ví dụ: đổi IP, CPU/RAM/ổ cứng tới mức độ nhất định (giống cloudwatch của AWS)
- hỗ trợ nhiều đích thông báo, ví dụ Lark/Slack/ntfy/telegram/... theo provider
- Sử dụng Go, không có UI

Mặc dù có nhiều ứng dụng monitoring ngoài kia, nhưng chúng không đạt so với yêu cầu của tôi:
- Chiếm nhiều resources hoặc chạy dạng agent gửi báo cáo về dịch vụ khác
- không đủ linh động
- tôi muốn sản phẩm tự chứa, bản thân nó có đủ các thứ để chạy, không cần phải cài đặt gì nhiều

Nguồn gốc tên: emday -> Em đây (tiếng Việt), nghĩa là khi tôi không biết IP của server giờ ở đâu thì nó tự báo cho tôi biết IP mới của nó -> em đây, IP này nè ;)
