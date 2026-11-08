package kz.yerek.aireply.debug

import android.app.Activity
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kz.yerek.aireply.billing.IapCatalogDto
import kz.yerek.aireply.billing.IapProductDto
import kz.yerek.aireply.billing.PurchaseApi
import kz.yerek.aireply.billing.PurchaseResultDto
import kz.yerek.aireply.billing.StoreClient
import kz.yerek.aireply.billing.StoreError
import kz.yerek.aireply.billing.StoreProduct
import kz.yerek.aireply.billing.StorePurchase
import kz.yerek.aireply.billing.StorePurchaseState
import kz.yerek.aireply.billing.StoreUpdate
import kz.yerek.aireply.data.account.UsageDto
import kz.yerek.aireply.data.account.WelcomeDto
import kz.yerek.aireply.data.support.SupportApi
import kz.yerek.aireply.data.support.SupportConversationDto
import kz.yerek.aireply.data.support.SupportMessageDto
import kz.yerek.aireply.data.support.SupportMessagesPage
import java.time.OffsetDateTime
import java.time.ZoneOffset
import java.time.temporal.ChronoUnit
import java.util.UUID

/**
 * DEBUG BUILDS ONLY: sample account data - a balance, a purchase catalogue
 * with a purchase that completes at once, and a support chat - so these
 * screens can be reviewed on an emulator without an account, Google Play or
 * the server. Chosen in Settings ▸ Developer and read when the app starts;
 * a release build never constructs any of it. The prices are samples, never
 * real ones, and nothing here leaves the phone.
 *
 * Тек жөндеу нұсқасында: үлгі баланс, өнімдер және чат, серверсіз.
 */
object DebugSamples {

    /** Seven of ten one-time welcome replies left, plus one permanent pack. */
    val usage: UsageDto
        get() = UsageDto(
            dailyLimit = 0,
            usedToday = 0,
            remainingToday = 0,
            resetsAt = now().plusHours(9).toString(),
            timezone = "Asia/Almaty",
            welcome = WelcomeDto(total = 10, remaining = 7),
            credits = 30,
            purchasedCredits = 30,
            adminCredits = 0,
            remainingTotal = 37
        )

    private fun now(): OffsetDateTime = OffsetDateTime.now(ZoneOffset.ofHours(5))

    // ------------------------------------------------------------ purchases

    private val catalog = IapCatalogDto(
        products = listOf(
            IapProductDto("base_monthly", "subscription", 0, 0, "debug.base.monthly", "Base", planId = "base_monthly", billingPeriod = "month", entitlementKind = "monthly", monthlyLimit = 1000),
            IapProductDto("base_yearly", "subscription", 0, 0, "debug.base.yearly", "Base", planId = "base_yearly", billingPeriod = "year", entitlementKind = "monthly", monthlyLimit = 1000),
            IapProductDto("pro_monthly", "subscription", 0, 0, "debug.pro.monthly", "Pro", planId = "pro_monthly", billingPeriod = "month", entitlementKind = "unlimited"),
            IapProductDto("pro_yearly", "subscription", 0, 0, "debug.pro.yearly", "Pro", planId = "pro_yearly", billingPeriod = "year", entitlementKind = "unlimited"),
            IapProductDto("pack30", "pack", 30, 0, "debug.pack30", "30 ответов")
        ),
        accountToken = "00000000-0000-4000-8000-000000000000"
    )

    private val prices = mapOf(
        "debug.base.monthly" to "990 ₸",
        "debug.base.yearly" to "9 990 ₸",
        "debug.pro.monthly" to "3 990 ₸",
        "debug.pro.yearly" to "39 900 ₸",
        "debug.pack30" to "130 ₸"
    )

    fun purchaseApi(): PurchaseApi = object : PurchaseApi {
        override suspend fun catalog(): IapCatalogDto = catalog
        override suspend fun verify(productId: String, purchaseToken: String) =
            PurchaseResultDto(status = PurchaseResultDto.STATUS_GRANTED, usage = usage)
    }

    fun storeClient(scope: CoroutineScope): StoreClient = object : StoreClient {
        private var listener: (StoreUpdate) -> Unit = {}

        override fun setListener(listener: (StoreUpdate) -> Unit) {
            this.listener = listener
        }

        override suspend fun connect() = true

        override suspend fun products(oneTime: List<String>, subscriptions: List<String>) =
            (oneTime + subscriptions).mapNotNull { id ->
                prices[id]?.let { price ->
                    StoreProduct(
                        productId = id,
                        formattedPrice = price,
                        billingPeriod = when {
                            id.endsWith("monthly") -> "month"
                            id.endsWith("yearly") -> "year"
                            else -> null
                        }
                    )
                }
            }

        override suspend fun purchases() = emptyList<StorePurchase>()

        override fun launch(activity: Activity, productId: String, accountToken: String, replacing: String?): StoreError? {
            scope.launch {
                delay(600)
                listener(
                    StoreUpdate.Purchases(
                        listOf(StorePurchase(listOf(productId), "debug-" + UUID.randomUUID(), StorePurchaseState.PURCHASED, accountToken))
                    )
                )
            }
            return null
        }
    }

    // --------------------------------------------------------- support chat

    /** A short conversation; what is sent here is kept in memory and answered by nobody. */
    fun supportApi(): SupportApi = object : SupportApi {
        private val messages = mutableListOf(
            message("user", "Здравствуйте! Клавиатура не вставляет скопированное сообщение.", minutesAgo = 42),
            message("operator", "Здравствуйте! Откройте AI Reply ▸ Настройки ▸ «Настройка клавиатуры и разрешений» и проверьте строку «Вставка».", minutesAgo = 38),
            message("operator", "Если там «Ещё не проверено», скопируйте сообщение заново и нажмите «Ответить».", minutesAgo = 37),
            message("user", "Спасибо, теперь работает!", minutesAgo = 5)
        )

        private fun message(sender: String, text: String, minutesAgo: Long) = SupportMessageDto(
            id = UUID.randomUUID().toString(),
            sender = sender,
            text = text,
            createdAt = now().minusMinutes(minutesAgo).truncatedTo(ChronoUnit.MINUTES).toString()
        )

        override suspend fun conversation() = SupportConversationDto(id = "debug", unreadCount = 0)

        override suspend fun messages(before: String?, after: String?, limit: Int): SupportMessagesPage = when {
            after != null -> SupportMessagesPage(messages.dropWhile { it.id != after }.drop(1))
            before != null -> SupportMessagesPage()
            else -> SupportMessagesPage(messages.toList())
        }

        override suspend fun send(text: String, clientMessageId: String): SupportMessageDto {
            delay(400)
            return message("user", text, minutesAgo = 0).also { messages += it }
        }

        override suspend fun markRead(upToMessageId: String) = 0
    }
}
