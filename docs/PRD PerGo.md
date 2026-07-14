# **Product Requirements Document: PerGo Enterprise Omnichannel CPaaS**

## **1\. Executive Summary**

Digital enterprise communications demand absolute structural reliability, elastic performance scaling, and strict cost controls. The platform codenamed PerGo addresses these requirements as a high-performance, open-source Omnichannel Communications Platform as a Service (CPaaS) engineered in Golang1. By delivering a single, unified backend gateway, PerGo abstracts the engineering complexities associated with managing fragmented APIs, divergent authentication frameworks, and unpredictable webhook payloads across modern communication channels1.  
The strategic objective of PerGo focuses on eliminating the high transactional markup costs imposed by proprietary market leaders such as Twilio, while simultaneously preventing vendor lock-in and safeguarding enterprise data sovereignty1. Designed for horizontal scalability, PerGo serves as a consolidated communication pool. The roadmap transitions the architecture from a functional multi-channel prototype in its initial phase to an operator-grade, carrier-resilient messaging gateway in its final production state.

## **2\. Problem and Opportunity**

Modern software organizations face significant friction when integrating communication channels into their product lines. Developers must construct custom, highly coupled API integrations for each external provider, reconciling divergent authentication protocols and inconsistent delivery receipt states1. Attempting to leverage commercial CPaaS consolidators introduces high marginal costs that scale with transaction volume, reducing profitability as customer interaction scales. Furthermore, using closed-source routing networks forces enterprises to send sensitive user information through third-party infrastructure, presenting compliance risks under rigorous data privacy regimes such as GDPR and LGPD.  
PerGo resolves these operational liabilities by delivering a unified, self-hosted messaging abstraction layer1. The platform translates a single standardized API request into channel-specific protocols while maintaining local custody of transaction histories and credential stores1. This architecture enables significant cost savings, secure on-premises deployment, and reliable horizontal scaling.

| Dimension | Legacy Commercial CPaaS | Bespoke Internal Integration | PerGo Platform |
| :---- | :---- | :---- | :---- |
| **Direct Message Markup** | High pay-per-message fees | Zero markup fees | Zero markup fees; complete cost control1 |
| **Data Privacy & Sovereignty** | Data processed externally | Full local data custody | Compliant, localized self-hosting1 |
| **API Integration Complexity** | Single vendor, rigid API schemas | Multi-provider API fragmentation | Unified JSON payload wrapper1 |
| **Session Control** | Strictly limited to official channels | Challenging to build and maintain | Native official and unofficial channel support1 |
| **Deployment Topology** | Multi-tenant public cloud only | Highly variable bespoke code | Standardized container orchestration1 |

## **3\. Objectives and Key Results**

The operational trajectory of PerGo is measured against quantitative key results designed to demonstrate high system efficiency and continuous platform reliability.

| Strategic Objective | Metric Description | Target Quantitative Key Result |
| :---- | :---- | :---- |
| **Optimize System Throughput** | Multi-channel message ingestion volume | Process greater than **500 API requests per second** under sustained peak loads1. |
| **Minimize Queue Ingestion Latency** | Network-to-broker transition speed | Maintain end-to-end ingestion latency under **50 milliseconds**1. |
| **Maximized Delivery Success Rate** | End-to-end channel delivery | Achieve greater than **99.5% delivery success** across all active provider channels. |
| **Immutable Traceability** | Security and compliance logging | Maintain **100% trace-correlated logging** for all incoming requests and outgoing webhooks1. |
| **System Resource Efficiency** | Compute footprint in baseline environment | Ensure memory consumption stays under **512MB RAM** on 2 vCPU runtime environments. |

## **4\. User Personas and Journeys**

### **The API Consumer (Backend Developer)**

The developer requires *a simple, predictable REST API* to integrate omnichannel communication flows into secondary software systems such as CRM, ERP, and customer support environments1. This user seeks *transparent authentication patterns*, stable JSON structures, and *reliable webhook delivery* to track message transitions like RECEIVED and READ across disparate messaging protocols1.

* **Developer Lifecycle Journey:**  
  1. The integration layer submits a single, standard JSON POST payload to the primary messaging endpoint containing target endpoints and message bodies1.  
  2. The system returns an immediate 202 Accepted response enclosing a cryptographically unique Trace-ID1.  
  3. The target gateway receives and processes the payload asynchronously, while the developer's server receives *real-time webhook updates* to track delivery progression1.

### **The SaaS Administrator (System Operator)**

The system administrator manages corporate workspaces, monitors connection health, provisions secure access credentials, and reviews compliance trails. This operator requires a *multi-tenant interface* to segregate corporate client spaces and view connection configurations1.

* **Operator Lifecycle Journey:**  
  1. The administrator provisions an isolated client workspace and generates cryptographically signed API authorization keys1.  
  2. The operator accesses the web console to pair an unofficial WhatsApp device by scanning a *live, dynamically refreshed QR code*1.  
  3. The console displays real-time connection telemetry, and the administrator configures fallback pathways and reviews audit logs to maintain strict operational compliance1.

## **5\. Functional Specifications**

\+---------------------------------------------------------------------------------------------------------+  
|                                             PerGo Core API                                             |  
|                                                                                                         |  
|   \+-----------------------+       Validate & Trace      \+-------------------------------------------+   |  
|   |   POST /api/v1/messages      | \--------------------------\> |  NATS JetStream (Work Queue Persistence)  |   |  
|   \+-----------------------+                             \+-------------------------------------------+   |  
\+---------------------------------------------------------------------------------------------------------+  
                                                                                    |  
                                                                                    | Dispatch to Workers  
                                                                                    v  
\+---------------------------------------------------------------------------------------------------------+  
|                                           Channel Workers                                               |  
|                                                                                                         |  
|   \+-------------------------+       \+---------------------------+       \+---------------------------+   |  
|   |  whatsmeow Worker       |       |  WhatsApp Cloud Worker    |       |  Telegram Worker          |   |  
|   |  (WhatsApp Web)         |       |  (WABA REST API)          |       |  (Telegram HTTP API)      |   |  
|   \+-------------------------+       \+---------------------------+       \+---------------------------+   |  
\+---------------------------------------------------------------------------------------------------------+

### **1\. Unified Message Ingestion Gateway**

The API interface provides a standard communication gateway. It validates payloads, generates unique trace identifiers, and queues requests for asynchronous processing1.

* **Functional Requirements:** The system must expose a unified POST /api/v1/messages REST endpoint1. The controller parses the JSON input and validates fields against predefined schemas before assigning a trace header and routing the payload to the queue.  
* **Go Architecture Note:** The ingestion gateway leverages the Echo web framework to handle HTTP requests with minimal overhead5. Incoming JSON data is parsed directly into strongly typed Go structs, with schema validation managed using structural tag assertions. Decoupling the HTTP delivery layer from individual channel integrations is achieved through a clean interface-based adapter pattern3:  
  Go  
  type MessageDispatcher interface {  
      Dispatch(ctx context.Context, payload \*MessagePayload) (\*DispatchReceipt, error)  
  }

  This design allows developers to implement new messaging integrations as independent adapters without modifying the central Echo route handlers5.

### **2\. Multi-Tenant Dashboard Control Panel**

To minimize operational overhead, the administrator interface uses a server-rendered architecture to manage workspaces and connection states in real time1.

* **Functional Requirements:** The interface provides a secure control panel where operators can manage isolated workspaces, monitor channel health, and pair unofficial WhatsApp devices using on-screen QR codes1.  
* **Go Architecture Note:** The frontend interface is built using a stack of Echo6, Templ7, and HTMX4. Rather than using standard runtime-interpreted templates, Templ compiles HTML structures directly into type-safe Go code, reducing rendering latency and allocation overhead4. The backend intercepts incoming requests to determine if the caller supports HTMX, allowing the server to return lightweight HTML fragments instead of complete pages4:  
  Go  
  if c.Request().Header.Get("HX-Request") \== "true" {  
      return component.Render(c.Request().Context(), c.Response().Writer)  
  }

  This server-driven approach minimizes frontend package size and simplifies client-side state synchronization, providing a *highly responsive pairing experience*4.

### **3\. Multi-Session Connection and Instance Controller**

The platform manages the lifecycles of multiple messaging channels, particularly unofficial WhatsApp Web integrations that run over persistent WebSocket channels1.

* **Functional Requirements:** The connection manager must safely isolate multiple linked devices, track session states, securely store persistent connection tokens, and trigger webhook alerts when sessions drop or recover1.  
* **Go Architecture Note:** Multi-session tracking is managed by an in-memory client registry using a sync.RWMutex map structure to ensure thread-safe read and write operations3. Active whatsmeow clients are run in independent goroutines to isolate their respective WebSockets and event handlers2. Connection sessions are stored securely using whatsmeow/store/sqlstore\#Container mapped directly to the central PostgreSQL database9. This ensures previously paired devices can automatically re-establish connections when the server restarts3:  
  Go  
  deviceStore, err := container.GetDevice(parsedJID)  
  if err \== nil && deviceStore \!= nil {  
      client := whatsmeow.NewClient(deviceStore, nil)  
      client.Connect()  
  }

  This approach prevents session degradation and ensures that paired instances remain connected3.

### **4\. Smart Queueing, Backpressure, and Rate-Limiting Engine**

The system protects downstream services and connected devices during API rate limits or connection drops by queueing outbound messages10.

* **Functional Requirements:** If a WhatsApp Web instance disconnects, outbound messages are held in a pending queue10. If the queue depth for that session exceeds **1,000 messages**, the API applies backpressure, rejecting subsequent requests with an HTTP 429 or 422 error10. For unofficial channels, the engine staggers delivery times (e.g., introducing a random **1 to 3 seconds** delay) to simulate human behavior and minimize the risk of provider account suspensions.  
* **Go Architecture Note:** The queueing infrastructure uses NATS JetStream with a WorkQueuePolicy stream, guaranteeing at-least-once delivery and ensuring each message is processed by only one worker10. When an instance disconnects, the corresponding channel worker halts message acknowledgements, leaving the pending payloads safely queued within JetStream10. The maximum queue limit is checked mathematically:  
  ![][image1]  
  If this limit is exceeded, the API returns an HTTP 429 response. Staggered dispatching is managed using rate limiters from the golang.org/x/time/rate package8:  
  Go  
  limiter := rate.NewLimiter(rate.Every(time.Second \* 2), 1)  
  if err := limiter.Wait(ctx); err \== nil {  
      // Execute the staggered message dispatch  
  }

  Using Wait blocks the queue worker goroutine asynchronously11. This releases thread execution context back to the Go runtime scheduler during delay states and allows thousands of independent queues to process concurrently15.

### **5\. Automated Smart Fallback Pipeline**

The routing engine provides resilient message delivery by automatically failing over to secondary communication channels when a primary transmission path is blocked1.

* **Functional Requirements:** The message schema supports a sorted array of fallback\_channels (e.g., \["whatsapp\_cloud", "telegram"\])1. If a primary channel (like the WhatsApp Business API) rejects a message (for instance, if the interactive template session window has expired), the routing engine catches the error and requeues the payload to the next available channel in the array1.  
* **Go Architecture Note:**  
  The fallback process is modeled as an iterative pipeline that handles errors and controls retries:  
  Go  
  func (engine \*RoutingEngine) ResolveDelivery(ctx context.Context, msg \*MessagePayload) error {  
      for \_, channel := range msg.FallbackChannels {  
          err := engine.dispatchers\[channel\].Dispatch(ctx, msg)  
          if err \== nil {  
              return nil // Delivery succeeded, exit pipeline  
          }  
          // Log dispatch failure and proceed to next fallback channel  
      }  
      return errors.New("delivery failed on all configured fallback channels")  
  }

  This design ensures a *smooth transition when failing over to a fallback channel*, maintaining delivery rates without exposing backend retries to the initiating system1.

### **6\. Compliance, Auditing, and Logging Engine**

For enterprise compliance, the system records an immutable audit log of all API transactions and delivery status transitions1.

* **Functional Requirements:** Every inbound API request must generate a unique Trace-ID1. All state changes (Queued, Sent, Delivered, Read, Failed) are written to an immutable audit\_logs table in PostgreSQL, partitioned by workspace\_id1. These logs must be accessible via both the API and the administration dashboard1.  
* **Go Architecture Note:** The platform uses context.Context to propagate the Trace-ID across boundary layers, including HTTP request contexts5, NATS JetStream headers17, background workers, and SQL database transactions. To prevent database write bottlenecks under heavy loads, logging operations are decoupled from main execution threads using buffered channel workers15:  
  Go  
  logBuffer := make(chan AuditLogEntry, 5000)

  A pool of background logging workers reads from this channel, batching inserts into PostgreSQL to protect database performance during high throughput spikes.

## **6\. Non-Functional Specifications**

The core architecture is designed to meet strict non-functional standards to ensure stability, efficiency, and compliance under enterprise-grade workloads.

### **Performance and Scalability**

The Go runtime scheduler enables the system to handle concurrent tasks with minimal hardware overhead15. Using NATS JetStream for queue management allows the platform to scale horizontally, with workers distributed across multiple nodes to balance the ingestion load13.

### **Security and Data Isolation**

Data isolation is enforced throughout the application's lifecycle. API calls must authenticate using SHA-256 hashed API keys. To ensure data privacy, connected channel tokens and WhatsApp session credentials are encrypted at rest using AES-256-GCM before database insertion.

### **Observability and Monitoring**

The platform exposes performance endpoints using the native net/http/pprof runtime profiling suite to monitor CPU utilization and identify memory leaks during load testing11. It also tracks key operational metrics, such as memory utilization, queue depths, and execution latencies1.

\+-----------------------------------------------------------------------------------------+  
|                                  Non-Functional Metrics                                 |  
|                                                                                         |  
|   Metric                        Target Benchmark              Go Implementation         |  
|   \-----------------------------------------------------------------------------------   |  
|   Throughput Rate               \>= 500 Messages/Sec           Lock-free Channels        |  
|   Ingestion Latency             \<= 50ms (p99)                 Direct Broker Hand-off    |  
|   Session Encryption            AES-256-GCM                   Standard crypto package   |  
|   Diagnostic Profiling          Real-time Profiling           net/http/pprof integration|  
\+-----------------------------------------------------------------------------------------+

## **7\. Excluded Capabilities**

The initial launch phase focuses exclusively on stabilizing core transactional messaging. The following capabilities are omitted from the MVP architecture:

* **Real-time Voice and WebRTC Orchestration:**  
  The platform will handle text, media, and structured template payloads, but will not support real-time audio calls, SIP trunking, or WebRTC signaling (e.g., using Pion libraries).  
* **Community and Group Management:** The API will not support group-level administrative features such as creating channels, managing member permissions, or linking announcement groups. It only supports direct message delivery using target Group JIDs2.  
* **Visual Conversation Flow Builders:** The system operates as a backend message router and will not include a drag-and-drop conversational designer or bot builder1. Dynamic chat logic must be implemented by external consumer applications that interact with PerGo's REST endpoints and webhooks1.

## **8\. Dependencies and Technical Risks**

The platform's operational dependencies present technical risks that must be addressed through architectural safeguards.

### **Unofficial Protocol Changes**

* **Risk:** Unofficial integrations (like the WhatsApp Web API powered by whatsmeow) are subject to breaking changes when underlying messaging platforms update their protocols2.  
* **Mitigation:** The connection manager is decoupled from the main engine. This allows developers to update underlying communication libraries without redeploying the core platform.

### **Downstream Rate Limiting**

* **Risk:** Rapid messaging rates on official channel endpoints (like Meta's WABA or Telegram) can trigger provider rate limits, leading to dropped messages.  
* **Mitigation:** The system uses NATS JetStream to handle rate limits asynchronously, queueing rejected messages and retrying them automatically10.

### **Logging Database Bottlenecks**

* **Risk:** High messaging throughput can cause write contention on the PostgreSQL audit tables, slowing down the API1.  
* **Mitigation:** The system uses buffered channels to batch log writes, protecting the database from write spikes during peak load.

| Technical Risk | Impact level | Probability | Architectural Mitigation |
| :---- | :---- | :---- | :---- |
| **Unofficial Protocol Updates** \[cite: 2\] | Critical | High | Maintain abstract interfaces to easily update adapter libraries3. |
| **Provider-Side Rate Limits** | High | High | Use token-bucket rate limiters and NATS JetStream retry queues8. |
| **Database Write Latency** | Medium | Medium | Use asynchronous background workers to batch write logs15. |
| **Connection Dropouts** | Medium | High | Use persistent connection workers with backoff-based retry logic3. |

## **9\. Post-Launch Success Metrics**

The performance and reliability of PerGo are tracked across a structured post-launch schedule to ensure the system meets its operational benchmarks.

### **30-Day Evaluation: System Stability**

The first 30 days focus on verifying the core architecture under initial production workloads.

* **Metrics:**  
  * Ingestion latency must remain below **50 milliseconds** at the 99th percentile under baseline loads1.  
  * All active API requests and delivery events must generate trace-correlated entries in the PostgreSQL database1.  
  * Establish connection stability, with zero database-driven session dropouts across linked devices.

### **60-Day Evaluation: Scaling and Recovery**

The 60-day milestone tests the platform's performance under heavy loads and simulated connection outages.

* **Metrics:**  
  * Confirm the system can process greater than **500 API requests per second** during peak traffic1.  
  * Verify that NATS JetStream safely queues and resumes pending messages during simulated channel disconnections10.  
  * Confirm that backpressure controls trigger HTTP 429 or 422 errors when queues exceed the **1,000-message limit**10.

### **90-Day Evaluation: Reliability and Cost Efficiency**

The 90-day review assesses the platform's long-term cost benefits and architectural stability.

* **Metrics:**  
  * Achieve a message delivery success rate of **99.5%** or higher across all active channels.  
  * Document cost savings compared to traditional pay-per-message API providers.  
  * Verify stable, long-term memory utilization (below **512MB RAM**) to confirm the absence of memory leaks in long-running background tasks16.

| Project Implementation Milestone | Key Deliverables | Timeline |
| :---- | :---- | :---- |
| **Milestone 1: Core Foundation** | Deploy the Echo API, set up PostgreSQL schemas, build the logging engine, and initialize the Templ-based control panel1. | Weeks 1 to 3 |
| **Milestone 2: Queue & WhatsApp Web** | Integrate NATS JetStream, implement the whatsmeow connection worker, set up rate limiting, and establish backpressure logic1. | Weeks 4 to 6 |
| **Milestone 3: Official Channel Integration** | Add WABA and Telegram integrations, set up the smart fallback engine, and perform load testing1. | Weeks 7 to 8 |

#### **Works cited**

1. gdbrns/go-whatsapp-multi-session-rest-api \- GitHub, [https://github.com/gdbrns/go-whatsapp-multi-session-rest-api](https://github.com/gdbrns/go-whatsapp-multi-session-rest-api)  
2. GitHub \- tulir/whatsmeow: Go library for the WhatsApp web multidevice API, [https://github.com/tulir/whatsmeow](https://github.com/tulir/whatsmeow)  
3. How can I manage many devices/sessions? · Issue \#786 · tulir/whatsmeow \- GitHub, [https://github.com/tulir/whatsmeow/issues/786](https://github.com/tulir/whatsmeow/issues/786)  
4. Building Reactive UIs with Go, Templ, and HTMX: A Simpler Path Beyond SPAs \- Medium, [https://medium.com/@iamsiddharths/building-reactive-uis-with-go-templ-and-htmx-a-simpler-path-beyond-spas-17e7dad2c7a2](https://medium.com/@iamsiddharths/building-reactive-uis-with-go-templ-and-htmx-a-simpler-path-beyond-spas-17e7dad2c7a2)  
5. Setup nested HTML template in Go Echo web framework \- DEV Community, [https://dev.to/ykyuen/setup-nested-html-template-in-go-echo-web-framework-e9b](https://dev.to/ykyuen/setup-nested-html-template-in-go-echo-web-framework-e9b)  
6. Building Reusable Modals with HTMX, templ, and Go \- The Murph's Blog, [https://themurph.hashnode.dev/building-reusable-modals-with-htmx-templ-and-go](https://themurph.hashnode.dev/building-reusable-modals-with-htmx-templ-and-go)  
7. Setting up HTMX and Templ for Go \- Tailbits, [https://tailbits.com/blog/setting-up-htmx-and-templ-for-go](https://tailbits.com/blog/setting-up-htmx-and-templ-for-go)  
8. Rate Limiting Strategies in Go: Token Bucket, Leaky Bucket, and Sliding Window, [https://dev.to/lovestaco/rate-limiting-strategies-in-go-token-bucket-leaky-bucket-and-sliding-window-3fnh](https://dev.to/lovestaco/rate-limiting-strategies-in-go-token-bucket-leaky-bucket-and-sliding-window-3fnh)  
9. feat: example of multiple sessions by mateusfmello · Pull Request \#471 · tulir/whatsmeow \- GitHub, [https://github.com/tulir/whatsmeow/pull/471](https://github.com/tulir/whatsmeow/pull/471)  
10. Streams \- NATS Docs, [https://docs.nats.io/nats-concepts/jetstream/streams](https://docs.nats.io/nats-concepts/jetstream/streams)  
11. How to Implement Rate Limiting in Go \- OneUptime, [https://oneuptime.com/blog/post/2026-01-23-go-rate-limiting/view](https://oneuptime.com/blog/post/2026-01-23-go-rate-limiting/view)  
12. Message still in nats limit queue after ack and term sent in Go \- Stack Overflow, [https://stackoverflow.com/questions/73699403/message-still-in-nats-limit-queue-after-ack-and-term-sent-in-go](https://stackoverflow.com/questions/73699403/message-still-in-nats-limit-queue-after-ack-and-term-sent-in-go)  
13. Consumers \- NATS Docs, [https://docs.nats.io/nats-concepts/jetstream/consumers](https://docs.nats.io/nats-concepts/jetstream/consumers)  
14. Go Wiki: Rate Limiting \- The Go Programming Language, [https://go.dev/wiki/RateLimiting](https://go.dev/wiki/RateLimiting)  
15. Rate Limiting \- Go by Example, [https://gobyexample.com/rate-limiting](https://gobyexample.com/rate-limiting)  
16. How to Build High-Concurrency Rate Limiters in Go (With Real Code Examples), [https://blog.stackademic.com/how-to-build-high-concurrency-rate-limiters-in-go-with-real-code-examples-d136e1d2fc6a](https://blog.stackademic.com/how-to-build-high-concurrency-rate-limiters-in-go-with-real-code-examples-d136e1d2fc6a)  
17. FAQ \- NATS Docs, [https://docs.nats.io/reference/faq](https://docs.nats.io/reference/faq)  
18. How to Implement Queue Groups in NATS \- OneUptime, [https://oneuptime.com/blog/post/2026-02-02-nats-queue-groups/view](https://oneuptime.com/blog/post/2026-02-02-nats-queue-groups/view)  
19. send.go · e7e263bf217551435ec668535801595a8d139ee9 · Tulir Asokan / whatsmeow · GitLab \- mau.dev, [https://mau.dev/tulir/whatsmeow/-/blob/e7e263bf217551435ec668535801595a8d139ee9/send.go](https://mau.dev/tulir/whatsmeow/-/blob/e7e263bf217551435ec668535801595a8d139ee9/send.go)

[image1]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAmwAAAA0CAYAAAA312SWAAADdElEQVR4Xu3cS6iuUxgH8OVWFHIriYGhTJwoDEi5RBkolPs5IQkRJkRCEkqRSxIGBlJC4oRDFBm4ThClpNwvAxzllAnP01rvOWuvs79zPrU/dvx+9e9d61nvqv3t0dN7KwUAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAGbYI/Jo5OehfnTkmzY+MXJ8t/ZkZOfIrpHXu3ruObKNN5WlewAAFmrPyIbIx5EPItctXV7VdomcOxabNZEfIp9EfhnW3ovc182fb8fTIn929Rwf1ca5Z/JE2bIHAGChsiG5eKiti9w81FaTQyL3tuM8XihbN2z5u88f5sdFPox82tXzytwrkbPaOZNrhjkAwEJko3b7WAz7l6VXk1aTkyI3RvYbF7ZhVsOWTVg/v6jU87Jpm3wf+TxybTtncsUwBwBYcfeX2Q3HoZG3xuK/7NXI3mNxTvM2bNnA/l6WNmxfR74o9TZx//+6dJgDAKy4fLZrVsORV5oeHIsLlA3i9hwR+Sly27gwh2zYfh1q+dvPHuanR76MfNTVvyv1ub5L2jmTy4c5AMCKyxcMsgEa5QP2/2QjcnDkjLG4HdksvR3ZbVyYIRu2jUMtf+PV3Tzf+tyrbP1GaZ53Z6l/Z/9/uaPUPQAAC5PPri3XmOXVpcu6+cmR1yKPRXYqdU8+7P9Z5KXIlaU2Qw+Xepsw5zlen5tLba5ujayN3FPq/lsiz7T1C0t94/KmNp/XDpHnynxX3LJh+22ovRF5pJs/3Y75jFz/f8nx4W2ceyb5hui0BwBgYQ6IvBh5p9Q3IbOhSvtuPqOUl7tx+rbUhu28Uhu2lMczN5+xZXxX5P1Sm7GnWu3Hdsz9k797hW1e+5TaZGXTlcnn4I7p1vN35DfXHupqacfIA6VebctxL5vP3HPYUAcAWLirIne38fVdPRu63ldl+YYtv182mcb5nbO8etbLZ8LS2LAd1M0BAFjGOaVehcqv/x/Y1U+NvFvq7c6Un7d4vCVvM64r9UH9N9t6znN8SptfELmh1O+W5ZuWf0ROiDzb1lLebs2rWduSty/zCtlyyZckAAD+83Yvtfk5dlwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD4H/kLiWOk0WIrLB8AAAAASUVORK5CYII=>