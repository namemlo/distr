import {OverlayModule} from '@angular/cdk/overlay';
import {DatePipe, TitleCasePipe} from '@angular/common';
import {Component, computed, ElementRef, inject, signal, viewChild} from '@angular/core';
import {toSignal} from '@angular/core/rxjs-interop';
import {FormControl, FormGroup, ReactiveFormsModule, Validators} from '@angular/forms';
import {ActivatedRoute, RouterLink} from '@angular/router';
import {AccountRole} from '@distr-sh/distr-sdk';
import {FaIconComponent} from '@fortawesome/angular-fontawesome';
import {faBoxesStacked, faChevronDown, faUsers} from '@fortawesome/free-solid-svg-icons';
import {combineLatest, firstValueFrom, map, startWith, Subject, switchMap} from 'rxjs';
import {AccessTokensTableComponent, AccessTokenStore} from '../access-tokens/access-tokens-table.component';
import {AutotrimDirective} from '../directives/autotrim.directive';
import {CustomerOrganizationsService} from '../services/customer-organizations.service';
import {ServiceAccountsService} from '../services/service-accounts.service';
import {ToastService} from '../services/toast.service';

@Component({
  selector: 'app-service-account-detail',
  imports: [
    AccessTokensTableComponent,
    AutotrimDirective,
    DatePipe,
    FaIconComponent,
    OverlayModule,
    ReactiveFormsModule,
    RouterLink,
    TitleCasePipe,
  ],
  templateUrl: './service-account-detail.component.html',
})
export class ServiceAccountDetailComponent {
  protected readonly faBoxesStacked = faBoxesStacked;
  protected readonly faChevronDown = faChevronDown;
  protected readonly faUsers = faUsers;

  private readonly route = inject(ActivatedRoute);
  private readonly service = inject(ServiceAccountsService);
  private readonly customerOrganizationsService = inject(CustomerOrganizationsService);
  private readonly toast = inject(ToastService);

  private readonly serviceAccountId$ = this.route.paramMap.pipe(map((p) => p.get('serviceAccountId') ?? ''));
  protected readonly serviceAccountId = toSignal(this.serviceAccountId$, {initialValue: ''});

  private readonly refreshSA$ = new Subject<void>();
  protected readonly serviceAccount = toSignal(
    combineLatest([this.serviceAccountId$, this.refreshSA$.pipe(startWith(0))]).pipe(
      switchMap(([id]) => this.service.get(id))
    )
  );

  private readonly allServiceAccounts = toSignal(this.service.list(), {initialValue: []});
  private readonly customerOrganizations = toSignal(this.customerOrganizationsService.getCustomerOrganizations(), {
    initialValue: [],
  });

  protected readonly customerOrganization = computed(() => {
    const sa = this.serviceAccount();
    if (!sa?.customerOrganizationId) {
      return undefined;
    }
    return this.customerOrganizations().find((co) => co.id === sa.customerOrganizationId);
  });

  protected detailRouterLink(sa: {id: string; customerOrganizationId?: string}): unknown[] {
    if (sa.customerOrganizationId) {
      return ['/', 'customers', sa.customerOrganizationId, 'service-accounts', sa.id];
    }
    return ['/', 'users', 'service-accounts', sa.id];
  }

  // Service accounts visible in the current scope: customer-scoped SAs of the same customer, or
  // vendor-scoped SAs when the current SA has no customer organization.
  protected readonly siblingServiceAccounts = computed(() => {
    const sa = this.serviceAccount();
    if (!sa) {
      return [];
    }
    return this.allServiceAccounts().filter((other) => other.customerOrganizationId === sa.customerOrganizationId);
  });

  protected readonly tokenStore = computed<AccessTokenStore>(() => {
    const id = this.serviceAccountId();
    return {
      list: () => this.service.listTokens(id),
      create: (request) => this.service.createToken(id, request),
      delete: (tokenId) => this.service.deleteToken(id, tokenId),
    };
  });

  protected readonly editForm = new FormGroup({
    name: new FormControl<string>('', {nonNullable: true, validators: [Validators.required]}),
    accountRole: new FormControl<AccountRole>('read_write', {nonNullable: true}),
  });
  protected editLoading = signal(false);
  protected editing = signal(false);

  protected readonly dropdownTriggerButton = viewChild.required<ElementRef<HTMLElement>>('dropdownTriggerButton');
  protected readonly breadcrumbDropdown = signal(false);
  protected breadcrumbDropdownWidth = 0;

  protected toggleBreadcrumbDropdown() {
    this.breadcrumbDropdown.update((v) => !v);
    if (this.breadcrumbDropdown()) {
      this.breadcrumbDropdownWidth = this.dropdownTriggerButton().nativeElement.getBoundingClientRect().width;
    }
  }

  public startEdit() {
    const sa = this.serviceAccount();
    if (!sa) {
      return;
    }
    this.editForm.reset({name: sa.name, accountRole: sa.accountRole});
    this.editing.set(true);
  }

  public cancelEdit() {
    this.editing.set(false);
  }

  public async saveEdit() {
    const sa = this.serviceAccount();
    if (!sa || this.editForm.invalid) {
      return;
    }
    this.editLoading.set(true);
    try {
      await firstValueFrom(
        this.service.patch(sa.id, {
          name: this.editForm.value.name,
          accountRole: this.editForm.value.accountRole,
        })
      );
      this.toast.success('Service account updated');
      this.editing.set(false);
      this.refreshSA$.next();
    } finally {
      this.editLoading.set(false);
    }
  }
}
